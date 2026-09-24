package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

type bullet struct {
	nodeID string
	taskID string
	text   string
}

// todoDoc builds a document with a TODO heading followed by the given bullets.
func todoDoc(items ...bullet) map[string]interface{} {
	lis := []interface{}{}
	for _, b := range items {
		attrs := map[string]interface{}{"nodeId": b.nodeID}
		if b.taskID != "" {
			attrs["taskId"] = b.taskID
		}
		text := b.text
		if text == "" {
			text = "item " + b.nodeID
		}
		lis = append(lis, map[string]interface{}{
			"type":  "listItem",
			"attrs": attrs,
			"content": []interface{}{map[string]interface{}{
				"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": text}},
			}},
		})
	}
	content := []interface{}{
		map[string]interface{}{"type": "heading", "attrs": map[string]interface{}{"level": 2},
			"content": []interface{}{map[string]interface{}{"type": "text", "text": "TODO"}}},
	}
	if len(lis) > 0 {
		content = append(content, map[string]interface{}{"type": "bulletList", "content": lis})
	}
	return map[string]interface{}{"type": "doc", "content": content}
}

func linkedTaskBody(pageID uuid.UUID, nodeID, title string) map[string]interface{} {
	return map[string]interface{}{
		"id":           uuid.New().String(),
		"title":        title,
		"sourcePageId": pageID.String(),
		"sourceNodeId": nodeID,
	}
}

func createLinkedTask(t *testing.T, h *Harness, userID, pageID uuid.UUID, nodeID string) model.Task {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/tasks", linkedTaskBody(pageID, nodeID, "task for "+nodeID), userID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return Decode[model.Task](t, w)
}

func sharePage(t *testing.T, h *Harness, pageID uuid.UUID, ownerID, withID uuid.UUID, permission string) {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
		"resourceType": "page",
		"resourceId":   pageID.String(),
		"sharedWithId": withID.String(),
		"permission":   permission,
	}, ownerID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

// writeDoc PUTs content using the page's current revision as the precondition.
func writeDoc(t *testing.T, h *Harness, userID, pageID uuid.UUID, doc map[string]interface{}) model.PageContent {
	t.Helper()
	body := map[string]interface{}{"content": doc}
	if w := h.Do(t, "GET", "/api/v1/pages/"+pageID.String()+"/content", nil, userID); w.Code == http.StatusOK {
		body["expectedRevision"] = Decode[model.PageContent](t, w).Revision
	}
	w := h.Do(t, "PUT", "/api/v1/pages/"+pageID.String()+"/content", body, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return Decode[model.PageContent](t, w)
}

func taskVisible(t *testing.T, h *Harness, userID, taskID uuid.UUID) bool {
	t.Helper()
	return h.Do(t, "GET", "/api/v1/tasks/"+taskID.String(), nil, userID).Code == http.StatusOK
}

func snapshot(t *testing.T, h *Harness, pageID uuid.UUID, epoch int, seq int64, doc map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	return h.DoService(t, "PUT", "/internal/collab/pages/"+pageID.String()+"/snapshot", map[string]interface{}{
		"epoch": epoch, "upToSeq": seq, "content": doc, "schemaVersion": 1,
	})
}

func currentContent(t *testing.T, h *Harness, userID, pageID uuid.UUID) model.PageContent {
	t.Helper()
	w := h.Do(t, "GET", "/api/v1/pages/"+pageID.String()+"/content", nil, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return Decode[model.PageContent](t, w)
}

// ─── Task ↔ bullet integrity ──────────────────────────────────────────────────

// TestTaskSourceIntegrity covers the task side effects that are unsafe once
// more than one client edits a page: creating a task for a bullet, and
// deleting it when the bullet goes away.
func TestTaskSourceIntegrity(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// The server, not the client, removes a task whose bullet is gone —
		// and it is a soft delete, so the bullet coming back restores it.
		"RemovingBulletSoftDeletesTaskAndRestoringBulletRestoresIt": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "n1", taskID: task.ID.String()}))

			// Customise the task so we can tell a restore from a re-create.
			w := h.Do(t, "PATCH", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{"status": "in-progress"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc()) // bullet cut
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID), "task must be hidden once its bullet is gone")

			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "n1", taskID: task.ID.String()})) // pasted back
			require.True(t, taskVisible(t, h, h.UserA.ID, task.ID), "task must be restored when its bullet returns")
			got := Decode[model.Task](t, h.Do(t, "GET", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID))
			assert.Equal(t, model.TaskStatus("in-progress"), got.Status, "restore must keep the task's fields")
		},

		"RemovedBulletsTaskIsHiddenFromListsAndBoards": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			keep := createLinkedTask(t, h, h.UserA.ID, page.ID, "keep")
			gone := createLinkedTask(t, h, h.UserA.ID, page.ID, "gone")
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "keep"}, bullet{nodeID: "gone"}))
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "keep"}))

			tasks := Decode[[]model.Task](t, h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID))
			ids := map[uuid.UUID]bool{}
			for _, tk := range tasks {
				ids[tk.ID] = true
			}
			assert.True(t, ids[keep.ID])
			assert.False(t, ids[gone.ID])

			bySource := Decode[[]model.Task](t, h.Do(t, "GET", "/api/v1/tasks?sourcePageId="+page.ID.String(), nil, h.UserA.ID))
			assert.Len(t, bySource, 1)
		},

		// An explicit delete is a user decision, not an inference from the
		// document, so a later content write must not undo it.
		"ExplicitDeleteIsNotUndoneByContentWrites": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "n1", taskID: task.ID.String()}))

			w := h.Do(t, "DELETE", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusNoContent, w.Code)

			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "n1", taskID: task.ID.String(), text: "edited"}))
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID))
		},

		// A second editor's content write reconciles the owner's tasks too:
		// the bullet is the source of truth whoever removed it.
		"AnotherEditorRemovingABulletSoftDeletesTheOwnersTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Shared plan")
			sharePage(t, h, page.ID, h.UserA.ID, h.UserB.ID, "editor")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "n1", taskID: task.ID.String()}))

			writeDoc(t, h, h.UserB.ID, page.ID, todoDoc())
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID))

			writeDoc(t, h, h.UserB.ID, page.ID, todoDoc(bullet{nodeID: "n1", taskID: task.ID.String()}))
			assert.True(t, taskVisible(t, h, h.UserA.ID, task.ID))
		},

		// Two clients (or a retry) creating the task for the same bullet get
		// the same task back rather than a duplicate.
		"CreatingATaskForTheSameBulletIsIdempotent": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			first := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")

			w := h.Do(t, "POST", "/api/v1/tasks", linkedTaskBody(page.ID, "n1", "second attempt"), h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, first.ID, Decode[model.Task](t, w).ID)

			bySource := Decode[[]model.Task](t, h.Do(t, "GET", "/api/v1/tasks?sourcePageId="+page.ID.String(), nil, h.UserA.ID))
			assert.Len(t, bySource, 1)
		},

		"RetryingTheSameCreateReturnsTheSameTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			body := linkedTaskBody(page.ID, "n1", "t")
			w1 := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w1.Code)
			w2 := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
			assert.Equal(t, Decode[model.Task](t, w1).ID, Decode[model.Task](t, w2).ID)
		},

		"ConcurrentCreatesForTheSameBulletProduceOneTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")

			const n = 16
			var wg sync.WaitGroup
			codes := make([]int, n)
			ids := make([]string, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					b, _ := json.Marshal(linkedTaskBody(page.ID, "race", fmt.Sprintf("attempt %d", i)))
					req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewReader(b))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Test-User-ID", h.UserA.ID.String())
					w := httptest.NewRecorder()
					h.Router.ServeHTTP(w, req)
					codes[i] = w.Code
					var tk model.Task
					_ = json.Unmarshal(w.Body.Bytes(), &tk)
					ids[i] = tk.ID.String()
				}(i)
			}
			wg.Wait()

			created := 0
			for i, c := range codes {
				require.Contains(t, []int{http.StatusCreated, http.StatusOK}, c, "attempt %d", i)
				if c == http.StatusCreated {
					created++
				}
				assert.Equal(t, ids[0], ids[i], "every attempt must resolve to the same task")
			}
			assert.Equal(t, 1, created)
			bySource := Decode[[]model.Task](t, h.Do(t, "GET", "/api/v1/tasks?sourcePageId="+page.ID.String(), nil, h.UserA.ID))
			assert.Len(t, bySource, 1)
		},

		// A collaborator racing to create the task for a bullet whose task is
		// private to its owner must not be handed that task.
		"AnotherUsersPrivateTaskIsNotDisclosed": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Shared plan")
			sharePage(t, h, page.ID, h.UserA.ID, h.UserB.ID, "editor")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{
				"title": "alice's secret task", "sourcePageId": page.ID.String(), "sourceNodeId": "n1", "isPrivate": true,
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			w = h.Do(t, "POST", "/api/v1/tasks", linkedTaskBody(page.ID, "n1", "bob's attempt"), h.UserB.ID)
			assert.Equal(t, http.StatusConflict, w.Code)
			assert.NotContains(t, w.Body.String(), "alice's secret task")
		},

		// A stale client re-PUTting a task whose bullet was removed must not
		// bring it back.
		"UpsertCannotResurrectASoftDeletedTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc(bullet{nodeID: "n1"}))
			writeDoc(t, h, h.UserA.ID, page.ID, todoDoc())

			w := h.Do(t, "PUT", "/api/v1/tasks/"+task.ID.String(), map[string]interface{}{
				"title": "stale", "sourcePageId": page.ID.String(), "sourceNodeId": "n1",
			}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID))
		},

		// Re-creating the task for a bullet whose task was soft-deleted
		// restores the original rather than minting a second one.
		"RecreatingForASoftDeletedBulletRestoresTheOriginal": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/tasks/"+task.ID.String(), nil, h.UserA.ID).Code)

			w := h.Do(t, "POST", "/api/v1/tasks", linkedTaskBody(page.ID, "n1", "again"), h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, task.ID, Decode[model.Task](t, w).ID)
		},
	})
}

// ─── Collaborative session guards ─────────────────────────────────────────────

// TestCollabWritePath covers the API side of the single-writer rule: while a
// page is attached to a collaborative session, only the collab service writes
// its content, and only for the current epoch.
func TestCollabWritePath(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"SessionReportsWhatTheCallerMayDo": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")

			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/collab", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			s := Decode[model.CollabSession](t, w)
			assert.True(t, s.Enabled)
			assert.True(t, s.CanWrite)
			assert.Equal(t, h.UserA.ID, s.UserID)
			assert.Equal(t, "Alice", s.Name)

			// Strangers learn nothing.
			w = h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/collab", nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)

			sharePage(t, h, page.ID, h.UserA.ID, h.UserB.ID, "viewer")
			w = h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/collab", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.False(t, Decode[model.CollabSession](t, w).CanWrite, "a viewer must join read-only")
		},

		"EditorShareMayWrite": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			sharePage(t, h, page.ID, h.UserA.ID, h.UserB.ID, "editor")
			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/collab", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.True(t, Decode[model.CollabSession](t, w).CanWrite)
		},

		"RestWriteToAnAttachedPageIsRefused": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			before := writeDoc(t, h, h.UserA.ID, page.ID, doc("before"))
			h.AttachCollab(t, page.ID)

			w := h.Do(t, "PUT", "/api/v1/pages/"+page.ID.String()+"/content",
				map[string]interface{}{"content": doc("REST CLOBBER"), "expectedRevision": before.Revision}, h.UserA.ID)
			require.Equal(t, http.StatusConflict, w.Code)
			assert.Equal(t, "collaborative", Decode[map[string]interface{}](t, w)["code"])
			assert.NotContains(t, string(currentContent(t, h, h.UserA.ID, page.ID).Content), "REST CLOBBER")
		},

		// The kill switch: with collaboration disabled a REST write detaches
		// the page, after which a lingering collab replica can't overwrite it.
		"RestWriteDetachesThePageWhenCollabIsDisabled": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			writeDoc(t, h, h.UserA.ID, page.ID, doc("before"))
			epoch := h.AttachCollab(t, page.ID)

			h.PageHandler.CollabEnabled = false
			defer func() { h.PageHandler.CollabEnabled = true }()
			writeDoc(t, h, h.UserA.ID, page.ID, doc("single-writer again"))

			w := snapshot(t, h, page.ID, epoch, 1, doc("late collab snapshot"))
			assert.Equal(t, http.StatusConflict, w.Code)
			assert.Contains(t, string(currentContent(t, h, h.UserA.ID, page.ID).Content), "single-writer again")
		},

		"SnapshotRequiresTheServiceToken": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			epoch := h.AttachCollab(t, page.ID)
			b, _ := json.Marshal(map[string]interface{}{"epoch": epoch, "upToSeq": 1, "content": doc("forged")})

			for _, auth := range []string{"", "Bearer wrong-token"} {
				req := httptest.NewRequest("PUT", "/internal/collab/pages/"+page.ID.String()+"/snapshot", bytes.NewReader(b))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Test-User-ID", h.UserA.ID.String())
				if auth != "" {
					req.Header.Set("Authorization", auth)
				}
				w := httptest.NewRecorder()
				h.Router.ServeHTTP(w, req)
				assert.Equal(t, http.StatusUnauthorized, w.Code, "auth %q", auth)
			}
		},

		"SnapshotWritesContentAndHistory": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			writeDoc(t, h, h.UserA.ID, page.ID, doc("seed"))
			epoch := h.AttachCollab(t, page.ID)

			w := snapshot(t, h, page.ID, epoch, 3, doc("from collab"))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Contains(t, string(currentContent(t, h, h.UserA.ID, page.ID).Content), "from collab")

			versions := Decode[[]model.PageContentVersion](t, h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content/versions", nil, h.UserA.ID))
			require.NotEmpty(t, versions)
			assert.Contains(t, string(versions[0].Content), "seed")
		},

		"IdenticalSnapshotsDoNotChurnHistory": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			epoch := h.AttachCollab(t, page.ID)
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 1, doc("same")).Code)
			rev := currentContent(t, h, h.UserA.ID, page.ID).Revision
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 2, doc("same")).Code)
			assert.Equal(t, rev, currentContent(t, h, h.UserA.ID, page.ID).Revision)
		},

		// A replica still holding a replaced document must not overwrite it.
		"SnapshotFromAReplacedEpochIsRejected": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			old := h.AttachCollab(t, page.ID)
			current := h.AttachCollab(t, page.ID)
			require.Greater(t, current, old)

			w := snapshot(t, h, page.ID, old, 100, doc("zombie"))
			require.Equal(t, http.StatusConflict, w.Code)
			assert.Equal(t, "stale_snapshot", Decode[map[string]interface{}](t, w)["code"])

			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, current, 1, doc("live")).Code)
		},

		"SnapshotCannotGoBackwards": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			epoch := h.AttachCollab(t, page.ID)
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 10, doc("newer")).Code)

			w := snapshot(t, h, page.ID, epoch, 5, doc("older"))
			assert.Equal(t, http.StatusConflict, w.Code)
			assert.Contains(t, string(currentContent(t, h, h.UserA.ID, page.ID).Content), "newer")
		},

		"SnapshotToAPageWithoutASessionIsRejected": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			assert.Equal(t, http.StatusConflict, snapshot(t, h, page.ID, 1, 1, doc("x")).Code)
		},

		// Snapshots get the same XSS sanitisation as REST writes.
		"SnapshotIsSanitised": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			epoch := h.AttachCollab(t, page.ID)
			evil := map[string]interface{}{"type": "doc", "content": []interface{}{
				map[string]interface{}{"type": "paragraph", "content": []interface{}{
					map[string]interface{}{"type": "text", "text": "click", "marks": []interface{}{
						map[string]interface{}{"type": "link", "attrs": map[string]interface{}{"href": "javascript:alert(1)"}},
					}},
				}},
			}}
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 1, evil).Code)
			assert.NotContains(t, string(currentContent(t, h, h.UserA.ID, page.ID).Content), "javascript:")
		},

		"SnapshotReconcilesTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			epoch := h.AttachCollab(t, page.ID)
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 1, todoDoc(bullet{nodeID: "n1"})).Code)
			require.True(t, taskVisible(t, h, h.UserA.ID, task.ID))

			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 2, todoDoc()).Code)
			assert.False(t, taskVisible(t, h, h.UserA.ID, task.ID))
		},

		// Restoring a version replaces the shared document: the session is
		// detached (so its replicas can no longer write) and tasks follow
		// the restored content.
		"RestoringAVersionDetachesTheSessionAndRestoresTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			task := createLinkedTask(t, h, h.UserA.ID, page.ID, "n1")
			epoch := h.AttachCollab(t, page.ID)
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 1, todoDoc(bullet{nodeID: "n1", text: "good"})).Code)
			require.Equal(t, http.StatusOK, snapshot(t, h, page.ID, epoch, 2, todoDoc()).Code)
			require.False(t, taskVisible(t, h, h.UserA.ID, task.ID))

			versions := Decode[[]model.PageContentVersion](t, h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content/versions", nil, h.UserA.ID))
			var goodID int64
			for _, v := range versions {
				if bytes.Contains(v.Content, []byte("good")) {
					goodID = v.ID
				}
			}
			require.NotZero(t, goodID)

			w := h.Do(t, "POST", fmt.Sprintf("/api/v1/pages/%s/content/versions/%d/restore", page.ID, goodID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Contains(t, string(currentContent(t, h, h.UserA.ID, page.ID).Content), "good")
			assert.True(t, taskVisible(t, h, h.UserA.ID, task.ID), "restoring the bullet must restore its task")

			// The old session can no longer write.
			assert.Equal(t, http.StatusConflict, snapshot(t, h, page.ID, epoch, 3, todoDoc()).Code)
		},

		"ViewersCannotRestoreVersions": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Plan")
			writeDoc(t, h, h.UserA.ID, page.ID, doc("one"))
			writeDoc(t, h, h.UserA.ID, page.ID, doc("two"))
			sharePage(t, h, page.ID, h.UserA.ID, h.UserB.ID, "viewer")
			versions := Decode[[]model.PageContentVersion](t, h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content/versions", nil, h.UserA.ID))
			require.NotEmpty(t, versions)

			w := h.Do(t, "POST", fmt.Sprintf("/api/v1/pages/%s/content/versions/%d/restore", page.ID, versions[0].ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusForbidden, w.Code)
		},
	})
}
