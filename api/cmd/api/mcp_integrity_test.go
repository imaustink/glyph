package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Data-integrity contract of the MCP content tools (DI-08).

// mcpPage creates a page through MCP and returns its id and the ids of the
// tasks its TODO bullets became.
func (e *mcpEnv) mcpPage(token, markdown string) (string, []string) {
	e.t.Helper()
	created := e.callOK(token, "create_page", map[string]interface{}{"title": "Plan", "markdown": markdown})
	pageID := created["page"].(map[string]interface{})["id"].(string)
	var tasks []string
	if tc, ok := created["tasks_created"].([]interface{}); ok {
		for _, t := range tc {
			tasks = append(tasks, t.(map[string]interface{})["id"].(string))
		}
	}
	return pageID, tasks
}

func (e *mcpEnv) revision(token, pageID string) int {
	e.t.Helper()
	got := e.callOK(token, "get_page", map[string]interface{}{"page_id": pageID})
	return int(got["revision"].(float64))
}

// storedContent reads the page's stored ProseMirror JSON through the app API.
func (e *mcpEnv) storedContent(pageID string) (string, int) {
	e.t.Helper()
	pc := e.session(e.alice, "GET", "/api/v1/pages/"+pageID+"/content", nil, http.StatusOK)
	b, err := json.Marshal(pc["content"])
	require.NoError(e.t, err)
	return string(b), int(pc["revision"].(float64))
}

func (e *mcpEnv) attachCollab(pageID string) {
	e.t.Helper()
	a, ok := e.s.pages.(interface{ AttachCollab(uuid.UUID) int })
	require.True(e.t, ok, "memstore page store can attach collab sessions")
	a.AttachCollab(uuid.MustParse(pageID))
}

func TestMCPReplaceRequiresExpectedRevision(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, nil, "")
	pageID, _ := e.mcpPage(g.access, "Hello\n")

	text, isErr := e.call(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "mode": "replace", "markdown": "Overwritten\n",
	})
	assert.True(t, isErr, "replace without expected_revision must be refused: %s", text)
	assert.Contains(t, text, "expected_revision")
	got := e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	assert.Equal(t, "Hello\n", got["markdown"])
}

func TestMCPReplaceRefusesToDropLinkedTasks(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, nil, "")
	pageID, tasks := e.mcpPage(g.access, "## TODO\n\n- Keep me\n- Drop me\n")
	require.Len(t, tasks, 2)

	// The agent dropped one bullet, and the marker of the other.
	text, isErr := e.call(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "mode": "replace", "expected_revision": e.revision(g.access, pageID),
		"markdown": "## TODO\n\n- [ ] Keep me\n",
	})
	assert.True(t, isErr, "dropping linked bullets must be refused: %s", text)
	assert.Contains(t, text, "allow_task_removal")
	assert.Contains(t, text, tasks[0])
	assert.Contains(t, text, tasks[1])
	for _, id := range tasks {
		e.callOK(g.access, "get_task", map[string]interface{}{"task_id": id})
	}
	list := e.callOK(g.access, "list_tasks", map[string]interface{}{})
	assert.EqualValues(t, 2, list["total"], "no duplicate task for the bullet that lost its marker")

	// Explicitly allowed: the dropped bullet's task goes, the kept one stays.
	e.callOK(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "mode": "replace", "expected_revision": e.revision(g.access, pageID),
		"allow_task_removal": true,
		"markdown":           "## TODO\n\n- [ ] Keep me <!-- task:" + tasks[0] + " -->\n",
	})
	e.callOK(g.access, "get_task", map[string]interface{}{"task_id": tasks[0]})
	_, isErr = e.call(g.access, "get_task", map[string]interface{}{"task_id": tasks[1]})
	assert.True(t, isErr, "the removed bullet's task is deleted with it")
}

func TestMCPReplaceKeepsIdentityAndFormatting(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, nil, "")
	pageID, _ := e.mcpPage(g.access, "x\n")
	other, otherTasks := e.mcpPage(g.access, "## TODO\n\n- Theirs\n")
	require.Len(t, otherTasks, 1)

	// Content with things Markdown can't express, written in the app.
	rev := e.revision(g.access, pageID)
	e.session(e.alice, "PUT", "/api/v1/pages/"+pageID+"/content", map[string]interface{}{
		"expectedRevision": rev,
		"content": json.RawMessage(`{"type":"doc","content":[` +
			`{"type":"paragraph","content":[{"type":"text","text":"underlined","marks":[{"type":"underline"}]}]},` +
			`{"type":"orderedList","attrs":{"start":4,"type":"a"},"content":[{"type":"listItem","attrs":{"nodeId":"ol-1"},"content":[{"type":"paragraph","content":[{"type":"text","text":"four"}]}]}]},` +
			`{"type":"bulletList","content":[` +
			`{"type":"listItem","attrs":{"nodeId":"b-1"},"content":[{"type":"paragraph","content":[{"type":"text","text":"same"}]}]},` +
			`{"type":"listItem","attrs":{"nodeId":"b-2"},"content":[{"type":"paragraph","content":[{"type":"text","text":"old text"}]}]}]}]}`),
	}, http.StatusOK)

	got := e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	md := got["markdown"].(string)
	assert.Contains(t, md, "4. four", "ordered list start is rendered")
	md = strings.Replace(md, "old text", "new text", 1)
	// A marker naming another page's task, and a duplicated bullet.
	md += "\n- [ ] stolen <!-- task:" + otherTasks[0] + " -->\n"
	e.callOK(g.access, "write_page_content", map[string]interface{}{
		"page_id": pageID, "mode": "replace", "expected_revision": int(got["revision"].(float64)), "markdown": md,
	})

	stored, _ := e.storedContent(pageID)
	assert.Contains(t, stored, `"underline"`, "unchanged paragraph keeps its underline")
	assert.Contains(t, stored, `"type":"a"`, "unchanged ordered list keeps its type")
	assert.Contains(t, stored, `"nodeId":"ol-1"`)
	assert.Contains(t, stored, `"nodeId":"b-1"`, "unchanged bullet keeps its nodeId")
	assert.Contains(t, stored, `"nodeId":"b-2"`, "edited bullet keeps its nodeId")
	assert.Contains(t, stored, "new text")
	assert.NotContains(t, stored, otherTasks[0], "another page's task link is dropped")

	theirs := e.callOK(g.access, "get_task", map[string]interface{}{"task_id": otherTasks[0]})
	assert.Equal(t, other, theirs["sourcePageId"], "the other page's task is untouched")
}

func TestMCPAppendDoesNotLinkExistingBullets(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, nil, "")
	pageID, _ := e.mcpPage(g.access, "x\n")
	// A TODO bullet the user left unlinked in the app.
	e.session(e.alice, "PUT", "/api/v1/pages/"+pageID+"/content", map[string]interface{}{
		"expectedRevision": e.revision(g.access, pageID),
		"content": json.RawMessage(`{"type":"doc","content":[` +
			`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"TODO"}]},` +
			`{"type":"bulletList","content":[{"type":"listItem","attrs":{"nodeId":"u-1"},"content":[{"type":"paragraph","content":[{"type":"text","text":"not a task"}]}]}]}]}`),
	}, http.StatusOK)

	out := e.callOK(g.access, "write_page_content", map[string]interface{}{"page_id": pageID, "markdown": "Plain paragraph."})
	assert.Nil(t, out["tasks_created"], "append must only link bullets it added")
	stored, _ := e.storedContent(pageID)
	assert.NotContains(t, stored, "taskId")

	// A bullet the append itself adds under TODO is still linked.
	out = e.callOK(g.access, "write_page_content", map[string]interface{}{"page_id": pageID, "markdown": "## TODO\n\n- new one\n"})
	require.Len(t, out["tasks_created"], 1)
}

func TestMCPCollabNoteWritesFailDistinctly(t *testing.T) {
	e := newMCPEnvWith(t, collabConfig{enabled: true, serviceToken: "svc"})
	g := e.connect(e.alice, true, nil, "")
	pageID, _ := e.mcpPage(g.access, "x\n")
	e.attachCollab(pageID)

	text, isErr := e.call(g.access, "write_page_content", map[string]interface{}{"page_id": pageID, "markdown": "more"})
	require.True(t, isErr)
	assert.Contains(t, text, "open for live editing")
	assert.NotContains(t, text, "retry", "retrying can't succeed while the note is open")

	// create_task: the task must not outlive its failed bullet write.
	text, isErr = e.call(g.access, "create_task", map[string]interface{}{"title": "Orphan?", "page_id": pageID})
	assert.True(t, isErr, "create_task must fail when the bullet can't be written: %s", text)
	assert.Contains(t, text, "open for live editing")
	list := e.callOK(g.access, "list_tasks", map[string]interface{}{})
	assert.EqualValues(t, 0, list["total"], "the task is rolled back")
}

func TestMCPGetPageCheckboxFollowsTask(t *testing.T) {
	e := newMCPEnv(t)
	g := e.connect(e.alice, true, nil, "")
	pageID, tasks := e.mcpPage(g.access, "## TODO\n\n- Ship it\n")
	require.Len(t, tasks, 1)

	e.callOK(g.access, "update_task", map[string]interface{}{"task_id": tasks[0], "status": "done"})
	got := e.callOK(g.access, "get_page", map[string]interface{}{"page_id": pageID})
	assert.Contains(t, got["markdown"], "- [x] Ship it <!-- task:"+tasks[0]+" -->")
}
