package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/pmmd"
	"github.com/google/uuid"
)

// currentSchemaVersion matches CURRENT_SCHEMA_VERSION in
// src/lib/editor/migrations/index.ts: documents this package writes use the
// editor's current ProseMirror schema.
const currentSchemaVersion = 1

type pageView struct {
	ID        uuid.UUID  `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Path      string     `json:"path"`
	ParentID  *uuid.UUID `json:"parentId"`
	Tags      []string   `json:"tags"`
	Priority  string     `json:"priority"`
	Workspace string     `json:"workspace"`
	URL       string     `json:"url"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// pageIndex is the caller's whole readable page tree, used to compute
// breadcrumb paths and folder descendants.
type pageIndex struct {
	byID     map[uuid.UUID]*model.Page
	children map[uuid.UUID][]*model.Page
	roots    []*model.Page
}

func (cc *callContext) loadPages() (*pageIndex, error) {
	var pages []*model.Page
	if err := cc.api.get("/pages", nil, &pages); err != nil {
		return nil, err
	}
	idx := &pageIndex{byID: map[uuid.UUID]*model.Page{}, children: map[uuid.UUID][]*model.Page{}}
	for _, p := range pages {
		idx.byID[p.ID] = p
	}
	for _, p := range pages {
		// A parent the token can't see (another workspace, not shared)
		// makes this page a root from the caller's point of view.
		if p.ParentID != nil && idx.byID[*p.ParentID] != nil {
			idx.children[*p.ParentID] = append(idx.children[*p.ParentID], p)
		} else {
			idx.roots = append(idx.roots, p)
		}
	}
	return idx, nil
}

func (idx *pageIndex) path(p *model.Page) string {
	parts := []string{p.Title}
	seen := map[uuid.UUID]bool{p.ID: true}
	for cur := p; cur.ParentID != nil; {
		parent := idx.byID[*cur.ParentID]
		if parent == nil || seen[parent.ID] {
			break
		}
		seen[parent.ID] = true
		parts = append(parts, parent.Title)
		cur = parent
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " / ")
}

// descendants returns every page below root (not including root).
func (idx *pageIndex) descendants(root uuid.UUID) []*model.Page {
	var out []*model.Page
	seen := map[uuid.UUID]bool{root: true}
	queue := []uuid.UUID{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, ch := range idx.children[id] {
			if !seen[ch.ID] {
				seen[ch.ID] = true
				out = append(out, ch)
				queue = append(queue, ch.ID)
			}
		}
	}
	return out
}

func (idx *pageIndex) nextOrder(parentID *uuid.UUID) int {
	siblings := idx.roots
	if parentID != nil {
		siblings = idx.children[*parentID]
	}
	next := 0
	for _, s := range siblings {
		if s.Order >= next {
			next = s.Order + 1
		}
	}
	return next
}

func (cc *callContext) viewPage(p *model.Page, idx *pageIndex) pageView {
	v := pageView{
		ID:        p.ID,
		Type:      string(p.Type),
		Title:     p.Title,
		Path:      p.Title,
		ParentID:  p.ParentID,
		Tags:      p.Tags,
		Priority:  string(p.Priority),
		Workspace: workspaceOf(p.OrgID),
		URL:       cc.srv.appURL("/notes/" + p.ID.String()),
		UpdatedAt: p.UpdatedAt,
	}
	if idx != nil {
		v.Path = idx.path(p)
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if v.Priority == "" {
		v.Priority = string(model.PriorityNone)
	}
	return v
}

func (cc *callContext) getPage(id uuid.UUID) (*model.Page, error) {
	var p model.Page
	if err := cc.api.get("/pages/"+id.String(), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (cc *callContext) getContent(id uuid.UUID) (*model.PageContent, error) {
	var pc model.PageContent
	if err := cc.api.get("/pages/"+id.String()+"/content", nil, &pc); err != nil {
		var ae *apiError
		// A page that has never been edited has no content row yet.
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			if _, perr := cc.getPage(id); perr == nil {
				return &model.PageContent{PageID: id, Content: json.RawMessage(`{"type":"doc","content":[]}`), Revision: 0}, nil
			}
		}
		return nil, err
	}
	return &pc, nil
}

// putContent writes doc guarded by the revision it was derived from, so a
// concurrent edit in the app is never silently overwritten.
func (cc *callContext) putContent(id uuid.UUID, doc json.RawMessage, schemaVersion, expectedRevision int) (*model.PageContent, error) {
	if schemaVersion < currentSchemaVersion {
		schemaVersion = currentSchemaVersion
	}
	body := map[string]interface{}{
		"content":       doc,
		"schemaVersion": schemaVersion,
	}
	if expectedRevision > 0 {
		body["expectedRevision"] = expectedRevision
	}
	var out model.PageContent
	if err := cc.api.put("/pages/"+id.String()+"/content", body, &out); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusConflict {
			if ae.Code == "collaborative" {
				return nil, errNoteOpen
			}
			return nil, errPageChanged
		}
		return nil, err
	}
	return &out, nil
}

// The two ways a content write can conflict. They need different handling:
// a changed page can be re-read and written again; a note open for live
// editing refuses every whole-document write until it's closed, so retrying
// straight away can only fail again.
const (
	errPageChanged = userError("the page was edited by someone else since it was read; call get_page again and retry")
	errNoteOpen    = userError("the note is open for live editing in Glyph, and its content can't be written through MCP while it is. Don't try again straight away: ask the user to close the note, or come back to it later")
)

// pageTasks returns the page's live linked tasks, or ok = false when this
// connection can't read tasks.
func (cc *callContext) pageTasks(pageID uuid.UUID) (tasks []*model.Task, ok bool, err error) {
	if !hasScope(cc.scope, model.ScopeTaskRead) {
		return nil, false, nil
	}
	if err := cc.api.get("/tasks", url.Values{"sourcePageId": {pageID.String()}}, &tasks); err != nil {
		return nil, false, err
	}
	return tasks, true, nil
}

func taskStatuses(tasks []*model.Task) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		out[t.ID.String()] = string(t.Status)
	}
	return out
}

// linkResult reports what linkTodoBullets did, for the tool's output.
type linkResult struct {
	created []*model.Task
	note    string
}

func (r *linkResult) addTo(out map[string]interface{}, cc *callContext) {
	if len(r.created) > 0 {
		views := make([]taskView, 0, len(r.created))
		for _, t := range r.created {
			views = append(views, cc.viewTask(t))
		}
		out["tasks_created"] = views
	}
	if r.note != "" {
		out["note"] = r.note
	}
}

// linkTodoBullets does what the editor does when a bullet appears under a
// page's TODO heading: create a task for each unlinked bullet and link the
// bullet to it. The editor only does this while someone types, so without
// this, bullets an agent writes would never become tasks. Call it on the doc
// about to be saved, then save it; if the save fails, pass the result to
// rollback so the new tasks don't outlive the bullets they were made for.
//
// Only bullets this write adds are linked: a bullet whose nodeId is in
// existing (pmmd.SettledListItemNodeIDs of the doc being replaced or appended
// to) was already there, and the user may have left it unlinked on purpose;
// that holds even when a replace edits its text. A bullet that was empty is
// not in existing, so a TODO written over it (a replace reuses its nodeId) is
// linked like any added bullet. With existing
// non-nil, a bullet that has no nodeId yet was already there too (every
// bullet this package parses gets one). A nil existing links every bullet,
// for a brand-new page.
func (cc *callContext) linkTodoBullets(page *model.Page, doc json.RawMessage, existing map[string]bool) (json.RawMessage, *linkResult, error) {
	res := &linkResult{}
	trigger := pmmd.TodoTrigger{}
	if tr := page.TodoTrigger; tr != nil {
		trigger = pmmd.TodoTrigger{Pattern: tr.Pattern, MatchMode: string(tr.MatchMode), BlockTypes: tr.BlockTypes}
	}
	var added map[string]bool
	if existing != nil {
		ids, err := pmmd.ListItemNodeIDs(doc)
		if err != nil {
			return nil, nil, err
		}
		added = map[string]bool{}
		for id := range ids {
			if !existing[id] {
				added[id] = true
			}
		}
	}
	doc, bullets, err := pmmd.FindUnlinkedTodoBullets(doc, trigger)
	if err != nil {
		return nil, nil, err
	}
	var todo []pmmd.TodoBullet
	for _, b := range bullets {
		if b.Text != "" && (added == nil || added[b.NodeID]) {
			todo = append(todo, b)
		}
	}
	if len(todo) == 0 {
		return doc, res, nil
	}
	if !hasScope(cc.scope, model.ScopeTaskWrite) {
		res.note = fmt.Sprintf("%d bullet(s) under the TODO heading weren't turned into tasks because this connection lacks task:write; they'll be offered as tasks when the note is next edited in Glyph", len(todo))
		return doc, res, nil
	}
	links := make(map[string]pmmd.TaskLink, len(todo))
	for _, b := range todo {
		status := model.StatusTodo
		if b.Checked {
			status = model.StatusDone
		}
		nodeID := b.NodeID
		task := model.Task{
			Title:        b.Text,
			Status:       status,
			Priority:     model.PriorityNone,
			Tags:         []string{},
			SourcePageID: &page.ID,
			SourceNodeID: &nodeID,
			OrgID:        page.OrgID,
		}
		var created model.Task
		if err := cc.api.post("/tasks", task, &created); err != nil {
			cc.rollback(res)
			return nil, nil, err
		}
		res.created = append(res.created, &created)
		links[b.NodeID] = pmmd.TaskLink{TaskID: created.ID.String(), Status: string(created.Status)}
	}
	doc, err = pmmd.LinkTodoBullets(doc, links)
	if err != nil {
		cc.rollback(res)
		return nil, nil, err
	}
	return doc, res, nil
}

// rollback deletes tasks linkTodoBullets created, best effort.
func (cc *callContext) rollback(res *linkResult) {
	if res == nil {
		return
	}
	for _, t := range res.created {
		_ = cc.api.delete("/tasks/" + t.ID.String())
	}
	res.created = nil
}

// saveWithTodoLinks links the TODO bullets doc adds (see linkTodoBullets for
// existing) and writes it, rolling the new tasks back if the write fails.
func (cc *callContext) saveWithTodoLinks(page *model.Page, doc json.RawMessage, schemaVersion, expectedRevision int, existing map[string]bool) (*model.PageContent, *linkResult, error) {
	linked, res, err := cc.linkTodoBullets(page, doc, existing)
	if err != nil {
		return nil, nil, err
	}
	pc, err := cc.putContent(page.ID, linked, schemaVersion, expectedRevision)
	if err != nil {
		cc.rollback(res)
		return nil, nil, err
	}
	return pc, res, nil
}

// parentArg parses an optional parent_id argument; "" means top level.
func parentArg(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := parseID("parent_id", raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func pageTools() []*tool {
	return []*tool{
		{
			name:        "list_pages",
			title:       "List pages",
			description: "List notes (pages) and folders in the page tree, with breadcrumb paths. Pass parent_id to list only what's inside a folder or page (recursively).",
			input: object(map[string]schema{
				"parent_id": str("Only list items below this page or folder."),
				"type":      enum("Only list pages or only folders.", "page", "folder"),
				"tag":       str("Only list items with this tag."),
				"limit":     integer("Maximum results (default 200).", 1, 1000),
			}),
			scopes:     []model.OAuthScope{model.ScopePageRead},
			readOnly:   true,
			idempotent: true,
			run:        listPages,
		},
		{
			name:        "get_page",
			title:       "Read page",
			description: "Read a note: its metadata, its content as Markdown, and the tasks linked to it. Bullets linked to tasks render as `- [ ] text <!-- task:ID -->`; keep those markers when rewriting content so the links survive. The returned revision guards write_page_content.",
			input: object(map[string]schema{
				"page_id": str("The page id."),
			}, "page_id"),
			scopes:     []model.OAuthScope{model.ScopePageRead},
			readOnly:   true,
			idempotent: true,
			run:        getPageTool,
		},
		{
			name:        "create_page",
			title:       "Create page",
			description: "Create a note (or folder) with optional Markdown content. New pages are private to the user even inside an org workspace. Each bullet under a heading named TODO becomes a task linked to that bullet, exactly as when typing in Glyph (the created tasks are returned).",
			input: object(map[string]schema{
				"title":     str("Page title."),
				"markdown":  str("Initial content as Markdown."),
				"parent_id": str("Folder or page to create it under (default: top level)."),
				"workspace": str(`"personal" or an org id (default: the parent's workspace, else personal). See list_workspaces.`),
				"type":      enum("page (default) or folder.", "page", "folder"),
				"tags":      strArray("Tags."),
				"priority":  enum("Note priority.", priorityValues...),
			}, "title"),
			scopes: []model.OAuthScope{model.ScopePageWrite},
			run:    createPage,
		},
		{
			name:        "update_page",
			title:       "Update page details",
			description: "Rename a page, change its tags or priority, or move it to another folder. Use write_page_content to change the content itself.",
			input: object(map[string]schema{
				"page_id":   str("The page id."),
				"title":     str("New title."),
				"tags":      strArray("Replacement tag list."),
				"priority":  enum("Note priority.", priorityValues...),
				"parent_id": str(`New parent folder/page id, or "" to move to the top level.`),
			}, "page_id"),
			scopes:     []model.OAuthScope{model.ScopePageWrite},
			idempotent: true,
			run:        updatePage,
		},
		{
			name:        "write_page_content",
			title:       "Write page content",
			description: `Change a note's content using Markdown. mode "append" (default) adds the Markdown to the end and leaves existing content untouched. mode "replace" rewrites the whole note and requires expected_revision from get_page — read the note first and keep the <!-- task:ID --> markers on bullets you keep. Blocks you leave unchanged keep formatting Markdown can't show (underline, link targets). A replace that would remove a bullet linked to a task is refused unless allow_task_removal is true, because removing the bullet deletes its task. New bullets under the TODO heading become linked tasks, as in the editor; existing unlinked bullets with text are left alone, even if you edit them (an empty bullet you fill in counts as new). Ticking a checkbox on an existing task doesn't change it; use update_task for status. If the note is open for live editing in Glyph, writes fail until it is closed; don't retry straight away.`,
			input: object(map[string]schema{
				"page_id":            str("The page id."),
				"markdown":           str("Markdown to append, or the full new content for replace."),
				"mode":               enum(`"append" (default) or "replace".`, "append", "replace"),
				"expected_revision":  integer("Revision from get_page; the write fails if the page changed since. Required for replace.", 0, 1<<31-1),
				"allow_task_removal": boolean("Replace only: allow removing bullets linked to tasks, which deletes those tasks. Default false."),
			}, "page_id", "markdown"),
			scopes: []model.OAuthScope{model.ScopePageWrite},
			run:    writePageContent,
		},
	}
}

func listPages(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		ParentID string `json:"parent_id"`
		Type     string `json:"type"`
		Tag      string `json:"tag"`
		Limit    int    `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 200
	}
	idx, err := cc.loadPages()
	if err != nil {
		return nil, err
	}
	var candidates []*model.Page
	if args.ParentID != "" {
		id, err := parseID("parent_id", args.ParentID)
		if err != nil {
			return nil, err
		}
		if idx.byID[id] == nil {
			return nil, userError("parent_id not found")
		}
		candidates = idx.descendants(id)
	} else {
		for _, p := range idx.byID {
			candidates = append(candidates, p)
		}
	}
	views := make([]pageView, 0, len(candidates))
	for _, p := range candidates {
		if args.Type != "" && string(p.Type) != args.Type {
			continue
		}
		if args.Tag != "" && !containsFold(p.Tags, args.Tag) {
			continue
		}
		views = append(views, cc.viewPage(p, idx))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Path < views[j].Path })
	total := len(views)
	if len(views) > limit {
		views = views[:limit]
	}
	return map[string]interface{}{"total": total, "returned": len(views), "pages": views}, nil
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

func getPageTool(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		PageID string `json:"page_id"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	id, err := parseID("page_id", args.PageID)
	if err != nil {
		return nil, err
	}
	p, err := cc.getPage(id)
	if err != nil {
		return nil, err
	}
	idx, err := cc.loadPages()
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"page": cc.viewPage(p, idx)}

	if p.Type == model.NodeTypeFolder {
		children := make([]pageView, 0)
		for _, ch := range idx.children[p.ID] {
			children = append(children, cc.viewPage(ch, idx))
		}
		out["children"] = children
		return out, nil
	}

	pc, err := cc.getContent(id)
	if err != nil {
		return nil, err
	}
	tasks, haveTasks, err := cc.pageTasks(id)
	if err != nil {
		return nil, err
	}
	doc := pc.Content
	if haveTasks {
		// Checkboxes show the task rows' status: the attribute stored on the
		// bullet goes stale when a task changes outside the editor.
		if doc, err = pmmd.ApplyTaskStatuses(doc, taskStatuses(tasks)); err != nil {
			return nil, err
		}
	}
	md, err := pmmd.ToMarkdown(doc)
	if err != nil {
		return nil, err
	}
	out["revision"] = pc.Revision
	out["markdown"] = md
	if p.TodoTrigger != nil {
		out["todoTrigger"] = p.TodoTrigger
	}
	if haveTasks {
		autoSortTasks(tasks)
		views := make([]taskView, 0, len(tasks))
		for _, t := range tasks {
			views = append(views, cc.viewTask(t))
		}
		out["tasks"] = views
	}
	return out, nil
}

func createPage(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Title     string   `json:"title"`
		Markdown  string   `json:"markdown"`
		ParentID  string   `json:"parent_id"`
		Workspace string   `json:"workspace"`
		Type      string   `json:"type"`
		Tags      []string `json:"tags"`
		Priority  string   `json:"priority"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	title, err := requireString("title", args.Title)
	if err != nil {
		return nil, err
	}
	nodeType := model.NodeTypePage
	if args.Type == string(model.NodeTypeFolder) {
		nodeType = model.NodeTypeFolder
	} else if args.Type != "" && args.Type != string(model.NodeTypePage) {
		return nil, userError(`type must be "page" or "folder"`)
	}
	if nodeType == model.NodeTypeFolder && args.Markdown != "" {
		return nil, userError("folders have no content; omit markdown")
	}
	if args.Priority != "" {
		if err := validEnum("priority", args.Priority, priorityValues); err != nil {
			return nil, err
		}
	}
	parentID, err := parentArg(args.ParentID)
	if err != nil {
		return nil, err
	}
	idx, err := cc.loadPages()
	if err != nil {
		return nil, err
	}
	var inherit **uuid.UUID
	if parentID != nil {
		parent := idx.byID[*parentID]
		if parent == nil {
			return nil, userError("parent_id not found")
		}
		inherit = &parent.OrgID
	}
	orgID, err := cc.resolveWorkspace(args.Workspace, inherit)
	if err != nil {
		return nil, err
	}

	var doc json.RawMessage
	if args.Markdown != "" {
		if doc, err = pmmd.FromMarkdown(args.Markdown); err != nil {
			return nil, userError("could not parse markdown: " + err.Error())
		}
	}

	body := model.Page{
		Type:     nodeType,
		Title:    title,
		ParentID: parentID,
		Order:    idx.nextOrder(parentID),
		Tags:     args.Tags,
		Priority: model.Priority(args.Priority),
		OrgID:    orgID,
	}
	var created model.Page
	if err := cc.api.post("/pages", body, &created); err != nil {
		return nil, err
	}
	idx.byID[created.ID] = &created
	out := map[string]interface{}{"page": cc.viewPage(&created, idx)}
	if doc != nil {
		pc, links, err := cc.saveWithTodoLinks(&created, doc, currentSchemaVersion, 0, nil)
		if err != nil {
			out["warning"] = "page created, but writing its content failed: " + err.Error()
			return out, nil
		}
		out["revision"] = pc.Revision
		links.addTo(out, cc)
	}
	return out, nil
}

func updatePage(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args map[string]json.RawMessage
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	var pageID string
	if err := json.Unmarshal(args["page_id"], &pageID); err != nil {
		return nil, userError("page_id is required")
	}
	id, err := parseID("page_id", pageID)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{}
	if v, ok := args["title"]; ok {
		var title string
		if err := json.Unmarshal(v, &title); err != nil || strings.TrimSpace(title) == "" {
			return nil, userError("title must be a non-empty string")
		}
		body["title"] = strings.TrimSpace(title)
	}
	if v, ok := args["tags"]; ok {
		var tags []string
		if err := json.Unmarshal(v, &tags); err != nil {
			return nil, userError("tags must be an array of strings")
		}
		if tags == nil {
			tags = []string{}
		}
		body["tags"] = tags
	}
	if v, ok := args["priority"]; ok {
		var pr string
		if err := json.Unmarshal(v, &pr); err != nil {
			return nil, userError("priority must be a string")
		}
		if err := validEnum("priority", pr, priorityValues); err != nil {
			return nil, err
		}
		body["priority"] = pr
	}
	if v, ok := args["parent_id"]; ok {
		var parent *string
		if err := json.Unmarshal(v, &parent); err != nil {
			return nil, userError("parent_id must be a string")
		}
		if parent == nil || strings.TrimSpace(*parent) == "" {
			body["parentId"] = nil
		} else {
			pid, err := parseID("parent_id", *parent)
			if err != nil {
				return nil, err
			}
			body["parentId"] = pid
		}
	}
	if len(body) == 0 {
		return nil, userError("nothing to update: pass title, tags, priority, or parent_id")
	}
	var updated model.Page
	if err := cc.api.patch("/pages/"+id.String(), body, &updated); err != nil {
		return nil, err
	}
	idx, err := cc.loadPages()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"page": cc.viewPage(&updated, idx)}, nil
}

func writePageContent(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		PageID           string `json:"page_id"`
		Markdown         string `json:"markdown"`
		Mode             string `json:"mode"`
		ExpectedRevision *int   `json:"expected_revision"`
		AllowTaskRemoval bool   `json:"allow_task_removal"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	id, err := parseID("page_id", args.PageID)
	if err != nil {
		return nil, err
	}
	mode := args.Mode
	if mode == "" {
		mode = "append"
	}
	if mode != "append" && mode != "replace" {
		return nil, userError(`mode must be "append" or "replace"`)
	}
	if mode == "append" && strings.TrimSpace(args.Markdown) == "" {
		return nil, userError("markdown is required")
	}
	// Replace rewrites the whole note, so it must be based on what the agent
	// read: without a revision, edits a user made since would be overwritten
	// and the tasks of bullets they added deleted.
	if mode == "replace" && args.ExpectedRevision == nil {
		return nil, userError(`mode "replace" requires expected_revision: pass the revision get_page returned, so edits made since you read the page aren't overwritten`)
	}

	current, err := cc.getContent(id)
	if err != nil {
		return nil, err
	}
	if args.ExpectedRevision != nil && *args.ExpectedRevision != current.Revision {
		return nil, userError("the page has changed since you read it (now at revision " +
			itoa(current.Revision) + "); call get_page again and retry")
	}
	existing, err := pmmd.SettledListItemNodeIDs(current.Content)
	if err != nil {
		return nil, err
	}

	out := map[string]interface{}{"page_id": id, "mode": mode}
	var next json.RawMessage
	if mode == "append" {
		next, err = pmmd.AppendMarkdown(current.Content, args.Markdown)
		if err != nil {
			return nil, userError("could not parse markdown: " + err.Error())
		}
	} else {
		if next, err = cc.replaceDoc(id, current.Content, args.Markdown, args.AllowTaskRemoval, out); err != nil {
			return nil, err
		}
	}
	page, err := cc.getPage(id)
	if err != nil {
		return nil, err
	}
	written, links, err := cc.saveWithTodoLinks(page, next, current.SchemaVersion, current.Revision, existing)
	if err != nil {
		return nil, err
	}
	out["revision"] = written.Revision
	out["url"] = cc.srv.appURL("/notes/" + id.String())
	links.addTo(out, cc)
	return out, nil
}

// replaceDoc builds the doc a replace writes: the Markdown merged against the
// current doc (so unchanged blocks keep their nodeIds, marks and attributes),
// with duplicate or foreign task links dropped and linked bullets showing
// their task's status. It refuses to remove a bullet that is linked to a live
// task unless allowRemoval: that deletes the task, and an agent that merely
// dropped a <!-- task:ID --> marker would otherwise also get a duplicate task
// for the same bullet.
func (cc *callContext) replaceDoc(id uuid.UUID, prev json.RawMessage, markdown string, allowRemoval bool, out map[string]interface{}) (json.RawMessage, error) {
	next, err := pmmd.FromMarkdown(markdown)
	if err == nil {
		next, err = pmmd.MergeReplace(prev, next)
	}
	if err != nil {
		return nil, userError("could not parse markdown: " + err.Error())
	}

	prevIDs, err := pmmd.TaskIDs(prev)
	if err != nil {
		return nil, err
	}
	tasks, haveTasks, err := cc.pageTasks(id)
	if err != nil {
		return nil, err
	}
	// Tasks a bullet here may link to: the page's live tasks when this
	// connection can read them, else the links the note already has.
	allowed := map[string]*model.Task{}
	if haveTasks {
		for _, t := range tasks {
			allowed[t.ID.String()] = t
		}
	} else {
		for _, tid := range prevIDs {
			allowed[tid] = nil
		}
	}
	next, dropped, err := pmmd.SanitizeTaskLinks(next, func(tid string) bool {
		_, ok := allowed[tid]
		return ok
	})
	if err != nil {
		return nil, err
	}
	if len(dropped) > 0 {
		out["task_links_dropped"] = dropped
	}

	nextIDs, err := pmmd.TaskIDs(next)
	if err != nil {
		return nil, err
	}
	kept := map[string]bool{}
	for _, tid := range nextIDs {
		kept[tid] = true
	}
	var removed []string
	for _, tid := range prevIDs {
		if _, live := allowed[tid]; live && !kept[tid] {
			removed = append(removed, tid)
		}
	}
	if len(removed) > 0 {
		if !allowRemoval {
			desc := make([]string, len(removed))
			for i, tid := range removed {
				desc[i] = tid
				if t := allowed[tid]; t != nil {
					desc[i] = fmt.Sprintf("%s (%q)", tid, t.Title)
				}
			}
			return nil, userError(fmt.Sprintf(
				"this replace would remove the bullets of %d linked task(s), which deletes the tasks: %s. "+
					"Keep each bullet you mean to keep with its <!-- task:ID --> marker, "+
					"or pass allow_task_removal: true to delete those tasks.",
				len(removed), strings.Join(desc, ", ")))
		}
		out["tasks_removed"] = removed
	}

	if haveTasks {
		if next, err = pmmd.ApplyTaskStatuses(next, taskStatuses(tasks)); err != nil {
			return nil, err
		}
	}
	return next, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
