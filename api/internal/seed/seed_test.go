package seed

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store/memstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { gin.SetMode(gin.TestMode) }

var seedTime = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func memStores() Stores {
	users, pages, tasks, lanes, templates, orgs, shares := memstore.NewStores()
	return Stores{Users: users, Pages: pages, Tasks: tasks, Lanes: lanes, Templates: templates, Orgs: orgs, Shares: shares}
}

// expectedCounts walks the dataset definition.
func expectedCounts() (folders, pages, tasks, lanes int) {
	var walk func(ns []treeNode)
	walk = func(ns []treeNode) {
		for _, n := range ns {
			if n.folder {
				folders++
			} else {
				pages++
			}
			tasks += len(n.tasks) + len(n.boardTasks)
			lanes += len(n.lanes)
			walk(n.children)
		}
	}
	walk(demoTree())
	return
}

func TestSeed_CreatesWorkspace(t *testing.T) {
	ctx := context.Background()
	s := memStores()

	res, err := Seed(ctx, s, seedTime)
	require.NoError(t, err)
	require.True(t, res.Created)

	folders, pages, tasks, lanes := expectedCounts()
	assert.Equal(t, folders, res.Folders)
	assert.Equal(t, pages, res.Pages)
	assert.Equal(t, tasks, res.Tasks)
	assert.Equal(t, lanes, res.Lanes)
	assert.Equal(t, len(demoTemplates()), res.Templates)
	assert.GreaterOrEqual(t, res.Pages, 5)
	assert.GreaterOrEqual(t, res.Tasks, 15)

	org, err := s.Orgs.GetByID(ctx, res.OrgID)
	require.NoError(t, err)
	assert.Equal(t, OrgName, org.Name)

	// Everything belongs to the org and is visible to its members.
	allPages, err := s.Pages.ListByUser(ctx, res.UserID)
	require.NoError(t, err)
	require.Len(t, allPages, folders+pages)
	for _, p := range allPages {
		require.NotNil(t, p.OrgID, p.Title)
		assert.Equal(t, res.OrgID, *p.OrgID, p.Title)
		assert.False(t, p.IsPrivate, p.Title)
	}
	allTasks, err := s.Tasks.ListByUser(ctx, res.UserID)
	require.NoError(t, err)
	require.Len(t, allTasks, tasks)
	statuses := map[model.TaskStatus]int{}
	for _, task := range allTasks {
		require.NotNil(t, task.OrgID, task.Title)
		assert.Equal(t, res.OrgID, *task.OrgID, task.Title)
		assert.False(t, task.IsPrivate, task.Title)
		statuses[task.Status]++
	}
	for _, st := range []model.TaskStatus{model.StatusTodo, model.StatusInProgress, model.StatusDone, model.StatusCancelled} {
		assert.Positive(t, statuses[st], "no %s task", st)
	}

	// Each note task is linked to a bullet in its note that carries its id.
	for _, task := range allTasks {
		if task.SourcePageID == nil {
			require.NotNil(t, task.FolderID, "standalone task %q is on no board", task.Title)
			continue
		}
		require.NotNil(t, task.SourceNodeID)
		pc, err := s.Pages.GetContent(ctx, *task.SourcePageID, res.UserID)
		require.NoError(t, err)
		item := findListItem(t, pc.Content, *task.SourceNodeID)
		require.NotNil(t, item, "bullet for %q missing", task.Title)
		attrs := item["attrs"].(map[string]any)
		assert.Equal(t, task.ID.String(), attrs["taskId"])
		assert.Equal(t, string(task.Status), attrs["taskStatus"])
	}

	folderLanes, err := s.Lanes.ListByFolder(ctx, res.FolderIDs["Product Launch"], res.UserID)
	require.NoError(t, err)
	assert.NotEmpty(t, folderLanes)

	templates, err := s.Templates.ListByUser(ctx, res.UserID)
	require.NoError(t, err)
	require.Len(t, templates, len(demoTemplates()))
	defaults := 0
	for _, tm := range templates {
		require.NotNil(t, tm.OrgID)
		if tm.IsDefault {
			defaults++
		}
	}
	assert.Equal(t, 1, defaults)
}

func TestSeed_SecondRunIsNoop(t *testing.T) {
	ctx := context.Background()
	s := memStores()

	first, err := Seed(ctx, s, seedTime)
	require.NoError(t, err)
	before := snapshot(t, s, first)

	second, err := Seed(ctx, s, seedTime.Add(48*time.Hour))
	require.NoError(t, err)
	assert.False(t, second.Created)
	assert.Equal(t, first.OrgID, second.OrgID)
	assert.Equal(t, first.UserID, second.UserID)
	assert.Equal(t, before, snapshot(t, s, first))

	orgs, err := s.Orgs.ListForUser(ctx, first.UserID)
	require.NoError(t, err)
	assert.Len(t, orgs, 1)
}

func TestSeed_IncompleteWorkspaceIsReported(t *testing.T) {
	ctx := context.Background()
	s := memStores()
	u, err := SeedUser(ctx, s.Users)
	require.NoError(t, err)
	_, err = s.Orgs.CreateWithOwner(ctx, &model.Organization{Name: pendingOrgName, CreatedBy: u.ID})
	require.NoError(t, err)

	_, err = Seed(ctx, s, seedTime)
	require.ErrorIs(t, err, ErrIncomplete)
}

// A user who signs in after the seed (and is added to the org, as the
// Postgres trigger does) can list and edit the demo notes, tasks and lanes.
func TestSeed_NewMemberCanReadAndEdit(t *testing.T) {
	ctx := context.Background()
	s := memStores()
	res, err := Seed(ctx, s, seedTime)
	require.NoError(t, err)

	email, name := "new@example.com", "New User"
	user, err := s.Users.Upsert(ctx, "google-123", "https://accounts.google.com", &email, &name)
	require.NoError(t, err)
	_, err = s.Orgs.AddMember(ctx, res.OrgID, user.ID, MemberRole)
	require.NoError(t, err)

	assertMemberCanUseDemo(t, s, res, user)
}

// A user who is not a member sees none of it.
func TestSeed_NonMemberSeesNothing(t *testing.T) {
	ctx := context.Background()
	s := memStores()
	_, err := Seed(ctx, s, seedTime)
	require.NoError(t, err)

	user, err := s.Users.Upsert(ctx, "outsider", "https://accounts.google.com", nil, nil)
	require.NoError(t, err)
	api := newClient(NewRouter(s, user))
	var pages []model.Page
	require.NoError(t, api.do(ctx, "GET", "/api/v1/pages", nil, &pages))
	assert.Empty(t, pages)
	var tasks []model.Task
	require.NoError(t, api.do(ctx, "GET", "/api/v1/tasks", nil, &tasks))
	assert.Empty(t, tasks)
}

// assertMemberCanUseDemo checks, through the API as user, that the demo
// workspace is fully visible and editable.
func assertMemberCanUseDemo(t *testing.T, s Stores, res *Result, user *model.User) {
	t.Helper()
	ctx := context.Background()
	api := newClient(NewRouter(s, user))
	folders, pages, tasks, _ := expectedCounts()

	var orgs []model.OrgWithRole
	require.NoError(t, api.do(ctx, "GET", "/api/v1/orgs", nil, &orgs))
	require.Len(t, orgs, 1)
	assert.Equal(t, OrgName, orgs[0].Name)
	assert.Equal(t, MemberRole, orgs[0].Role)

	var pageList []model.Page
	require.NoError(t, api.do(ctx, "GET", "/api/v1/pages", nil, &pageList))
	assert.Len(t, pageList, folders+pages)

	var taskList []model.Task
	require.NoError(t, api.do(ctx, "GET", "/api/v1/tasks", nil, &taskList))
	assert.Len(t, taskList, tasks)

	var templates []model.Template
	require.NoError(t, api.do(ctx, "GET", "/api/v1/templates", nil, &templates))
	assert.Len(t, templates, len(demoTemplates()))

	// Edit a note: title and content.
	welcomeID := res.PageIDs["Welcome to Glyph"].String()
	require.NoError(t, api.do(ctx, "PATCH", "/api/v1/pages/"+welcomeID, map[string]any{"title": "Welcome (edited)"}, nil))
	var pc model.PageContent
	require.NoError(t, api.do(ctx, "GET", "/api/v1/pages/"+welcomeID+"/content", nil, &pc))
	var docJSON map[string]any
	require.NoError(t, json.Unmarshal(pc.Content, &docJSON))
	docJSON["content"] = append(docJSON["content"].([]any), paragraph(text("Edited by a preview user.")))
	require.NoError(t, api.do(ctx, "PUT", "/api/v1/pages/"+welcomeID+"/content",
		map[string]any{"content": docJSON, "expectedRevision": pc.Revision, "schemaVersion": 1}, nil))

	// Edit a note task and a board task.
	for _, task := range taskList {
		require.NoError(t, api.do(ctx, "PATCH", "/api/v1/tasks/"+task.ID.String(),
			map[string]any{"status": "done"}, nil), task.Title)
	}

	// Edit a folder board lane, and add a note to a demo folder.
	folderID := res.FolderIDs["Product Launch"].String()
	var folder struct {
		CanEdit bool `json:"canEdit"`
	}
	require.NoError(t, api.do(ctx, "GET", "/api/v1/folders/"+folderID, nil, &folder))
	assert.True(t, folder.CanEdit)
	var lanes []model.Lane
	require.NoError(t, api.do(ctx, "GET", "/api/v1/folders/"+folderID+"/lanes", nil, &lanes))
	require.NotEmpty(t, lanes)
	require.NoError(t, api.do(ctx, "PUT", "/api/v1/folders/"+folderID+"/lanes/"+lanes[0].ID.String(),
		map[string]any{"title": "Renamed lane"}, nil))
	var boardTasks []model.Task
	require.NoError(t, api.do(ctx, "GET", "/api/v1/folders/"+folderID+"/tasks", nil, &boardTasks))
	assert.NotEmpty(t, boardTasks)
	require.NoError(t, api.do(ctx, "POST", "/api/v1/pages",
		map[string]any{"type": "page", "title": "My note", "parentId": folderID}, nil))
}

// snapshot captures the seed user's data, for before/after comparisons.
func snapshot(t *testing.T, s Stores, res *Result) map[string]int {
	t.Helper()
	ctx := context.Background()
	pages, err := s.Pages.ListByUser(ctx, res.UserID)
	require.NoError(t, err)
	tasks, err := s.Tasks.ListByUser(ctx, res.UserID)
	require.NoError(t, err)
	templates, err := s.Templates.ListByUser(ctx, res.UserID)
	require.NoError(t, err)
	members, err := s.Orgs.ListMembers(ctx, res.OrgID)
	require.NoError(t, err)
	lanes := 0
	for _, id := range res.FolderIDs {
		ls, err := s.Lanes.ListByFolder(ctx, id, res.UserID)
		require.NoError(t, err)
		lanes += len(ls)
	}
	return map[string]int{
		"pages": len(pages), "tasks": len(tasks), "templates": len(templates),
		"members": len(members), "lanes": lanes,
	}
}

func findListItem(t *testing.T, content json.RawMessage, nodeID string) map[string]any {
	t.Helper()
	var root map[string]any
	require.NoError(t, json.Unmarshal(content, &root))
	var found map[string]any
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if found != nil {
			return
		}
		if n["type"] == "listItem" {
			if attrs, ok := n["attrs"].(map[string]any); ok && attrs["nodeId"] == nodeID {
				found = n
				return
			}
		}
		children, _ := n["content"].([]any)
		for _, c := range children {
			if m, ok := c.(map[string]any); ok {
				walk(m)
			}
		}
	}
	walk(root)
	return found
}
