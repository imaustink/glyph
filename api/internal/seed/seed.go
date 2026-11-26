// Package seed fills a fresh Glyph database with a demo workspace, for
// per-PR preview environments.
//
// The data is created through the real HTTP handlers, executed in-process
// against a gin router (the way /mcp executes tool calls), acting as a
// dedicated "Glyph Demo" user. That way seeded notes, tasks and lanes pass
// the same validation and derivations as anything a user creates — note
// tasks are linked to their bullets exactly as the editor links them — and
// the seed keeps working as the schema and handlers evolve.
//
// Everything lives in one organization, "Glyph Demo", owned by the seed
// user. Run (postgres.go) additionally installs a trigger that makes every
// user created afterwards (i.e. everyone who signs in to the preview) an
// editor of that organization, which is what gives them read/write access.
package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

const (
	// UserSub and UserIssuer identify the seed user. The issuer is not a
	// real OIDC issuer, so nobody can ever sign in as this user.
	UserSub    = "glyph-demo-seed"
	UserIssuer = "urn:glyph:demo-seed"
	userEmail  = "demo@glyph.invalid"
	userName   = "Glyph Demo"

	// OrgName is the demo organization's name once seeding has finished.
	OrgName = "Glyph Demo"
	// pendingOrgName is its name while seeding is in progress. The rename to
	// OrgName is the last step, so a run that died half-way is recognisable
	// (ErrIncomplete) and never mistaken for a finished dataset.
	pendingOrgName = "Glyph Demo (setting up)"

	// MemberRole is the role users get in the demo organization: enough to
	// read and edit every demo note, task, folder board and template, not to
	// administer the organization.
	MemberRole = model.OrgRoleEditor
)

// ErrIncomplete is returned by Seed when an earlier run started the demo
// workspace but did not finish it.
var ErrIncomplete = errors.New("seed: a previous demo seed did not finish")

// Stores are the stores the seed writes through.
type Stores struct {
	Users     store.UserStore
	Pages     store.PageStore
	Tasks     store.TaskStore
	Lanes     store.LaneStore
	Templates store.TemplateStore
	Orgs      store.OrgStore
	Shares    store.ShareStore
}

// Result describes a seed run.
type Result struct {
	// Created is false when the demo workspace already existed and the run
	// changed nothing.
	Created    bool
	UserID     uuid.UUID
	OrgID      uuid.UUID
	Folders    int
	Pages      int
	Tasks      int
	Lanes      int
	Templates  int
	FolderIDs  map[string]uuid.UUID
	PageIDs    map[string]uuid.UUID
	TaskTitles []string
}

func (r *Result) String() string {
	if !r.Created {
		return fmt.Sprintf("demo workspace already present (org %s); nothing to do", r.OrgID)
	}
	return fmt.Sprintf("created demo workspace (org %s): %d folders, %d notes, %d tasks, %d lanes, %d templates",
		r.OrgID, r.Folders, r.Pages, r.Tasks, r.Lanes, r.Templates)
}

// SeedUser returns the seed user, creating it if needed.
func SeedUser(ctx context.Context, users store.UserStore) (*model.User, error) {
	email, name := userEmail, userName
	u, err := users.Upsert(ctx, UserSub, UserIssuer, &email, &name)
	if err != nil {
		return nil, fmt.Errorf("seed user: %w", err)
	}
	return u, nil
}

// FindOrg returns the seed user's demo organization (finished or not), or
// nil if there is none.
func FindOrg(ctx context.Context, orgs store.OrgStore, seedUserID uuid.UUID) (*model.OrgWithRole, error) {
	list, err := orgs.ListForUser(ctx, seedUserID)
	if err != nil {
		return nil, fmt.Errorf("list seed user's orgs: %w", err)
	}
	for _, o := range list {
		if o.CreatedBy == seedUserID && (o.Name == OrgName || o.Name == pendingOrgName) {
			return o, nil
		}
	}
	return nil, nil
}

// Seed creates the demo workspace unless it already exists, in which case
// it changes nothing (Result.Created = false). It returns ErrIncomplete if
// an earlier run left a half-built workspace behind; the caller decides how
// to clean that up (see Run).
//
// now anchors the tasks' due dates (a few days either side of it).
func Seed(ctx context.Context, s Stores, now time.Time) (*Result, error) {
	user, err := SeedUser(ctx, s.Users)
	if err != nil {
		return nil, err
	}
	existing, err := FindOrg(ctx, s.Orgs, user.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Name != OrgName {
			return nil, ErrIncomplete
		}
		return &Result{Created: false, UserID: user.ID, OrgID: existing.ID}, nil
	}

	b := &builder{
		api: newClient(NewRouter(s, user)),
		now: now,
		res: &Result{
			Created:   true,
			UserID:    user.ID,
			FolderIDs: map[string]uuid.UUID{},
			PageIDs:   map[string]uuid.UUID{},
		},
	}
	if err := b.build(ctx); err != nil {
		return nil, err
	}
	return b.res, nil
}

// builder issues the requests that create the dataset.
type builder struct {
	api   *client
	now   time.Time
	res   *Result
	orgID uuid.UUID
	order int
}

func (b *builder) build(ctx context.Context) error {
	var org model.OrgWithRole
	if err := b.api.do(ctx, "POST", "/api/v1/orgs", map[string]any{"name": pendingOrgName}, &org); err != nil {
		return fmt.Errorf("create org: %w", err)
	}
	b.orgID = org.ID
	b.res.OrgID = org.ID

	for _, n := range demoTree() {
		if err := b.node(ctx, n, nil); err != nil {
			return err
		}
	}
	for _, t := range demoTemplates() {
		if err := b.template(ctx, t); err != nil {
			return err
		}
	}

	// Last: marks the workspace finished.
	if err := b.api.do(ctx, "PATCH", "/api/v1/orgs/"+org.ID.String(), map[string]any{"name": OrgName}, nil); err != nil {
		return fmt.Errorf("finish org: %w", err)
	}
	return nil
}

// node creates a folder or note (and, recursively, its children).
func (b *builder) node(ctx context.Context, n treeNode, parentID *uuid.UUID) error {
	nodeType := model.NodeTypePage
	if n.folder {
		nodeType = model.NodeTypeFolder
	}
	body := map[string]any{
		"type":     nodeType,
		"title":    n.title,
		"parentId": parentID,
		"order":    n.order,
		"tags":     nonNil(n.tags),
		"orgId":    b.orgID,
	}
	if n.priority != "" {
		body["priority"] = n.priority
	}
	if !n.folder {
		body["todoTrigger"] = defaultTodoTrigger()
	}
	var page model.Page
	if err := b.api.do(ctx, "POST", "/api/v1/pages", body, &page); err != nil {
		return fmt.Errorf("create %s %q: %w", nodeType, n.title, err)
	}
	// Pages are created private; share it with the organization before
	// anything is attached to it, so its tasks inherit the visibility.
	if err := b.api.do(ctx, "PATCH", "/api/v1/pages/"+page.ID.String(), map[string]any{"isPrivate": false}, nil); err != nil {
		return fmt.Errorf("share %q with org: %w", n.title, err)
	}

	if n.folder {
		b.res.Folders++
		b.res.FolderIDs[n.title] = page.ID
	} else {
		b.res.Pages++
		b.res.PageIDs[n.title] = page.ID
		if err := b.noteContent(ctx, page.ID, n); err != nil {
			return err
		}
	}

	for _, t := range n.boardTasks {
		if err := b.boardTask(ctx, page.ID, t); err != nil {
			return err
		}
	}
	for _, l := range n.lanes {
		if err := b.lane(ctx, page.ID, l); err != nil {
			return err
		}
	}
	for _, child := range n.children {
		if err := b.node(ctx, child, &page.ID); err != nil {
			return err
		}
	}
	return nil
}

// noteContent creates the note's bullet-linked tasks, then writes its
// document with each TODO bullet carrying its task's identity — the state
// the editor leaves a note in after it has created the tasks.
func (b *builder) noteContent(ctx context.Context, pageID uuid.UUID, n treeNode) error {
	linked := make([]linkedTask, len(n.tasks))
	for i, spec := range n.tasks {
		nodeID := newNodeID()
		body := b.taskBody(spec)
		body["sourcePageId"] = pageID
		body["sourceNodeId"] = nodeID
		var task model.Task
		if err := b.api.do(ctx, "POST", "/api/v1/tasks", body, &task); err != nil {
			return fmt.Errorf("create task %q on %q: %w", spec.title, n.title, err)
		}
		linked[i] = linkedTask{nodeID: nodeID, task: &task}
		b.res.Tasks++
		b.res.TaskTitles = append(b.res.TaskTitles, task.Title)
	}
	doc := n.content(func(i int) pmNode { return linked[i].listItem() })
	body := map[string]any{"content": doc, "schemaVersion": 1}
	if err := b.api.do(ctx, "PUT", "/api/v1/pages/"+pageID.String()+"/content", body, nil); err != nil {
		return fmt.Errorf("write content of %q: %w", n.title, err)
	}
	return nil
}

// boardTask creates a standalone task on a folder's board.
func (b *builder) boardTask(ctx context.Context, folderID uuid.UUID, spec taskSpec) error {
	body := b.taskBody(spec)
	body["folderId"] = folderID
	body["orgId"] = b.orgID
	body["isPrivate"] = false
	var task model.Task
	if err := b.api.do(ctx, "POST", "/api/v1/tasks", body, &task); err != nil {
		return fmt.Errorf("create board task %q: %w", spec.title, err)
	}
	b.res.Tasks++
	b.res.TaskTitles = append(b.res.TaskTitles, task.Title)
	return nil
}

func (b *builder) taskBody(spec taskSpec) map[string]any {
	b.order++
	body := map[string]any{
		"title":       spec.title,
		"description": spec.description,
		"status":      spec.status,
		"priority":    spec.priority,
		"tags":        nonNil(spec.tags),
		"order":       b.order,
	}
	if spec.due != nil {
		body["dueDate"] = b.now.AddDate(0, 0, *spec.due).Format("2006-01-02")
	}
	if spec.link != nil {
		body["link"] = spec.link
	}
	return body
}

func (b *builder) lane(ctx context.Context, folderID uuid.UUID, l laneSpec) error {
	body := map[string]any{
		"title":      l.title,
		"filterSet":  l.filter,
		"sortConfig": l.sort,
		"order":      l.order,
	}
	if err := b.api.do(ctx, "POST", "/api/v1/folders/"+folderID.String()+"/lanes", body, nil); err != nil {
		return fmt.Errorf("create lane %q: %w", l.title, err)
	}
	b.res.Lanes++
	return nil
}

func (b *builder) template(ctx context.Context, t templateSpec) error {
	body := map[string]any{
		"name":          t.name,
		"content":       mustJSON(t.content),
		"titleTemplate": t.titleTemplate,
		"todoTrigger":   defaultTodoTrigger(),
		"isDefault":     t.isDefault,
		"orgId":         b.orgID,
		"isPrivate":     false,
	}
	if err := b.api.do(ctx, "POST", "/api/v1/templates", body, nil); err != nil {
		return fmt.Errorf("create template %q: %w", t.name, err)
	}
	b.res.Templates++
	return nil
}

// linkedTask is a TODO bullet and the task created for it.
type linkedTask struct {
	nodeID string
	task   *model.Task
}

// listItem renders the bullet as the editor stores a task-linked bullet.
func (l linkedTask) listItem() pmNode {
	status := string(l.task.Status)
	checked := l.task.Status == model.StatusDone || l.task.Status == model.StatusCancelled
	return pmNode{
		"type": "listItem",
		"attrs": map[string]any{
			"nodeId":     l.nodeID,
			"taskId":     l.task.ID.String(),
			"checked":    checked,
			"taskStatus": status,
		},
		"content": []pmNode{paragraph(text(l.task.Title))},
	}
}

func defaultTodoTrigger() model.TodoTriggerConfig {
	return model.TodoTriggerConfig{Pattern: "TODO", MatchMode: "exact", BlockTypes: []string{"heading"}}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// trimLines joins lines with spaces, for long paragraphs in the dataset.
func trimLines(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
