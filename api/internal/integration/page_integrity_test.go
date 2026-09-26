package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrityPool exposes the Postgres backend's pool to specs that must look
// at rows the API no longer (or never) returns. Specs call it via
// rawPool and skip on backends without SQL.
func (b *postgresBackend) integrityPool() *pgxpool.Pool { return b.pool }

type rawSQLBackend interface{ integrityPool() *pgxpool.Pool }

// rawPool returns the backend's SQL pool, or skips the spec on memstore.
func rawPool(t *testing.T, h *Harness) *pgxpool.Pool {
	t.Helper()
	b, ok := h.Backend.(rawSQLBackend)
	if !ok {
		t.Skipf("backend %s has no SQL pool", h.Backend.Name())
	}
	return b.integrityPool()
}

// rawJSON decodes a response body into a generic map, so a spec can tell an
// absent key from a zero value.
func rawJSON(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	m := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(body, &m))
	return m
}

// assertTriggerUnset fails unless the JSON body has no todoTrigger key.
func assertTriggerUnset(t *testing.T, body []byte, msg string) {
	t.Helper()
	if v, ok := rawJSON(t, body)["todoTrigger"]; ok {
		t.Errorf("%s: todoTrigger = %s, want it absent (unset)", msg, v)
	}
}

func TestPageIntegrity(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── DI-07: an omitted todoTrigger must stay "unset" ──────────────
		// A nil *TodoTriggerConfig used to be stored as JSONB null and read
		// back as an empty config, which disabled TODO detection.
		"DI07_PageWithoutTodoTriggerRoundTripsAsUnset": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": "No trigger", "type": "page"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "create response")
			id := Decode[struct{ ID string }](t, w).ID

			w = h.Do(t, "GET", "/api/v1/pages/"+id, nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "get response")

			// An unrelated PATCH must not persist an empty config either.
			w = h.Do(t, "PATCH", "/api/v1/pages/"+id, map[string]interface{}{"title": "Renamed"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "patch response")

			// Nor a PUT without the field.
			w = h.Do(t, "PUT", "/api/v1/pages/"+id, map[string]interface{}{"title": "Put", "type": "page"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "put response")
		},

		"DI07_TemplateWithoutTodoTriggerRoundTripsAsUnset": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "No trigger"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "create response")
			id := Decode[struct{ ID string }](t, w).ID

			w = h.Do(t, "GET", "/api/v1/templates/"+id, nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "get response")
		},

		// Rows already written as JSONB null (before the fix and its backfill)
		// must read back as unset too.
		"DI07_LegacyJSONNullTodoTriggerReadsAsUnset": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := rawPool(t, h)
			page := createTestPage(t, h, h.UserA.ID, "Legacy")
			_, err := pool.Exec(context.Background(),
				`UPDATE pages SET todo_trigger = 'null'::jsonb WHERE id = $1`, page.ID)
			require.NoError(t, err)

			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assertTriggerUnset(t, w.Body.Bytes(), "legacy row")
		},

		// ── DI-06: page-tree cycles ──────────────────────────────────────
		"DI06_PutSelfParentRejected": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "F")
			w := h.Do(t, "PUT", "/api/v1/pages/"+f.ID.String(),
				map[string]interface{}{"title": "F", "type": "folder", "parentId": f.ID.String()}, h.UserA.ID)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Nil(t, getPage(t, h, h.UserA.ID, f.ID).ParentID)
		},

		"DI06_PutUnderOwnDescendantRejected": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "F")
			c := createChild(t, h, h.UserA.ID, f.ID, "folder", "C")
			w := h.Do(t, "PUT", "/api/v1/pages/"+f.ID.String(),
				map[string]interface{}{"title": "F", "type": "folder", "parentId": c.ID.String()}, h.UserA.ID)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Nil(t, getPage(t, h, h.UserA.ID, f.ID).ParentID)
		},

		// Two moves that are each fine alone (A under B, B under A) must not
		// both succeed when they race.
		"DI06_ConcurrentCrossMovesCannotFormCycle": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for round := 0; round < 40; round++ {
				a := createFolder(t, h, h.UserA.ID, "A")
				b := createFolder(t, h, h.UserA.ID, "B")
				var wg sync.WaitGroup
				start := make(chan struct{})
				move := func(id, parent uuid.UUID) {
					defer wg.Done()
					<-start
					h.Do(t, "PATCH", "/api/v1/pages/"+id.String(),
						map[string]interface{}{"parentId": parent.String()}, h.UserA.ID)
				}
				wg.Add(2)
				go move(a.ID, b.ID)
				go move(b.ID, a.ID)
				close(start)
				wg.Wait()

				ga, gb := getPage(t, h, h.UserA.ID, a.ID), getPage(t, h, h.UserA.ID, b.ID)
				cycle := ga.ParentID != nil && *ga.ParentID == b.ID && gb.ParentID != nil && *gb.ParentID == a.ID
				require.False(t, cycle, "round %d: concurrent moves stored a cycle A→B→A", round)
			}
		},

		// A cycle already in the data (written before the check existed) must
		// not make the recursive tree queries run forever.
		"DI06_TreeQueriesTerminateOnExistingCycle": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := rawPool(t, h)
			a := createFolder(t, h, h.UserA.ID, "A")
			b := createChild(t, h, h.UserA.ID, a.ID, "folder", "B")
			_, err := pool.Exec(context.Background(), `UPDATE pages SET parent_id = $1 WHERE id = $2`, b.ID, a.ID)
			require.NoError(t, err)

			ps := store.NewPageStore(pool)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			isAnc, err := ps.IsAncestor(ctx, uuid.New(), a.ID)
			require.NoError(t, err, "IsAncestor on a cycle")
			assert.False(t, isAnc)

			ids, err := ps.GetDescendantIDs(ctx, a.ID)
			require.NoError(t, err, "GetDescendantIDs on a cycle")
			assert.ElementsMatch(t, []uuid.UUID{a.ID, b.ID}, ids)

			rows, err := pool.Query(ctx, store.DescendantPagesSQL(), a.ID)
			require.NoError(t, err)
			n := 0
			for rows.Next() {
				n++
			}
			rows.Close()
			require.NoError(t, rows.Err(), "DescendantPagesSQL on a cycle")
			assert.Equal(t, 2, n)
		},

		// ── DI-05: concurrent PATCHes of different fields both stick ─────
		"DI05_ConcurrentPatchesOfDifferentFieldsBothPersist": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for round := 0; round < 40; round++ {
				p := createPage(t, h, h.UserA.ID, "Original")
				var wg sync.WaitGroup
				start := make(chan struct{})
				patch := func(body map[string]interface{}) {
					defer wg.Done()
					<-start
					h.Do(t, "PATCH", "/api/v1/pages/"+p.ID.String(), body, h.UserA.ID)
				}
				wg.Add(3)
				go patch(map[string]interface{}{"title": "Renamed"})
				go patch(map[string]interface{}{"tags": []string{"work"}})
				go patch(map[string]interface{}{"priority": "high"})
				close(start)
				wg.Wait()

				got := getPage(t, h, h.UserA.ID, p.ID)
				require.Equal(t, "Renamed", got.Title, "round %d: title lost", round)
				require.Equal(t, []string{"work"}, got.Tags, "round %d: tags lost", round)
				require.Equal(t, model.PriorityHigh, got.Priority, "round %d: priority lost", round)
			}
		},

		// ── PUT /pages/:id must not reset fields it wasn't given ─────────
		// Omitting isPrivate used to make a private page org-visible, and
		// order/tags/priority/todoTrigger were reset to zero values.
		"PutOmittedFieldsKeepTheirValues": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "F")
			w := h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{
				"title": "Full", "type": "page", "parentId": f.ID.String(), "order": 7, "tags": []string{"t"},
				"priority": "high", "todoTrigger": map[string]interface{}{"pattern": "TODO", "matchMode": "prefix", "blockTypes": []string{"listItem"}},
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			p := Decode[model.Page](t, w)
			require.True(t, p.IsPrivate)

			w = h.Do(t, "PUT", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{"title": "Renamed", "type": "page"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			got := getPage(t, h, h.UserA.ID, p.ID)
			assert.Equal(t, "Renamed", got.Title)
			assert.True(t, got.IsPrivate, "omitted isPrivate must not make the page visible")
			assert.Equal(t, 7, got.Order)
			assert.Equal(t, []string{"t"}, got.Tags)
			assert.Equal(t, model.PriorityHigh, got.Priority)
			if assert.NotNil(t, got.TodoTrigger) {
				assert.Equal(t, "TODO", got.TodoTrigger.Pattern)
			}
			if assert.NotNil(t, got.ParentID) {
				assert.Equal(t, f.ID, *got.ParentID)
			}

			// Explicit values (including null parent) still replace.
			w = h.Do(t, "PUT", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{
				"title": "Renamed", "type": "page", "isPrivate": false, "parentId": nil, "tags": []string{},
			}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			got = getPage(t, h, h.UserA.ID, p.ID)
			assert.False(t, got.IsPrivate)
			assert.Nil(t, got.ParentID)
			assert.Equal(t, []string{}, got.Tags)
		},

		// A page created through PUT is private unless it says otherwise,
		// like POST.
		"PutCreateDefaultsToPrivate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			id := uuid.New()
			w := h.Do(t, "PUT", "/api/v1/pages/"+id.String(), map[string]interface{}{"title": "New", "type": "page"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.True(t, getPage(t, h, h.UserA.ID, id).IsPrivate)
		},

		// ── DI-02: folder delete must not destroy other users' work ──────
		"DI02_DeleteFolderContainingOtherUsersPageRefused": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "Alice's folder")
			shareFolder(t, h, f.ID.String(), h.UserA.ID, h.UserB.ID, "editor")
			sub := createChild(t, h, h.UserA.ID, f.ID, "folder", "Sub")
			// The editor share lets Bob create his own page in Alice's folder.
			bobs := createChild(t, h, h.UserB.ID, f.ID, "page", "Bob's page")

			w := h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, "subtree_has_other_owners", Decode[map[string]interface{}](t, w)["code"])

			// Nothing was deleted.
			getPage(t, h, h.UserB.ID, bobs.ID)
			getPage(t, h, h.UserA.ID, sub.ID)
			getPage(t, h, h.UserA.ID, f.ID)
		},

		// Deleting a folder the caller wholly owns still removes the subtree.
		"DI02_DeleteOwnFolderRemovesDescendants": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "F")
			child := createChild(t, h, h.UserA.ID, f.ID, "page", "Child")

			w := h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			w = h.Do(t, "GET", "/api/v1/pages/"+child.ID.String(), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, "descendant should be deleted with its folder")
		},

		// Content history outlives the page, and the page's last content is
		// archived into it, so a deleted note stays recoverable.
		"DI02_DeleteKeepsContentHistory": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := rawPool(t, h)
			f := createFolder(t, h, h.UserA.ID, "F")
			p := createChild(t, h, h.UserA.ID, f.ID, "page", "Note")
			_, pc := putContent(t, h, p.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("first")})
			code, _ := putContent(t, h, p.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("second"), "expectedRevision": pc.Revision})
			require.Equal(t, http.StatusOK, code)

			w := h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String(), nil, h.UserA.ID)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

			var texts []string
			rows, err := pool.Query(context.Background(),
				`SELECT content #>> '{content,0,content,0,text}' FROM page_content_versions WHERE page_id = $1 ORDER BY id`, p.ID)
			require.NoError(t, err)
			for rows.Next() {
				var s string
				require.NoError(t, rows.Scan(&s))
				texts = append(texts, s)
			}
			rows.Close()
			assert.Equal(t, []string{"first", "second"}, texts, "history and last content must survive the delete")
		},

		// ── DI-21: deleting a note/folder must not orphan its tasks ──────
		"DI21_DeleteFolderSoftDeletesItsTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			f := createFolder(t, h, h.UserA.ID, "F")
			note := createChild(t, h, h.UserA.ID, f.ID, "page", "Note")
			writeDoc(t, h, h.UserA.ID, note.ID, todoDoc(bullet{nodeID: "n1"}))
			linked := createLinkedTask(t, h, h.UserA.ID, note.ID, "n1")
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "board task", "folderId": f.ID.String()}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			boardTask := Decode[model.Task](t, w)
			other := Decode[model.Task](t, h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "unrelated"}, h.UserA.ID))

			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String(), nil, h.UserA.ID).Code)

			assert.False(t, taskVisible(t, h, h.UserA.ID, linked.ID), "the note's task must be deleted with the note")
			assert.False(t, taskVisible(t, h, h.UserA.ID, boardTask.ID), "the folder board's task must be deleted with the folder")
			assert.True(t, taskVisible(t, h, h.UserA.ID, other.ID), "unrelated tasks are untouched")
		},

		// The delete is a soft delete, so the tasks can be recovered.
		"DI21_DeletedNoteTasksAreSoftDeleted": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := rawPool(t, h)
			note := createPage(t, h, h.UserA.ID, "Note")
			writeDoc(t, h, h.UserA.ID, note.ID, todoDoc(bullet{nodeID: "n1"}))
			linked := createLinkedTask(t, h, h.UserA.ID, note.ID, "n1")

			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/pages/"+note.ID.String(), nil, h.UserA.ID).Code)

			var reason *string
			var deleted bool
			require.NoError(t, pool.QueryRow(context.Background(),
				`SELECT deleted_at IS NOT NULL, deleted_reason FROM tasks WHERE id = $1`, linked.ID,
			).Scan(&deleted, &reason))
			assert.True(t, deleted, "task row must be kept, soft-deleted")
			if assert.NotNil(t, reason) {
				assert.Equal(t, "source_removed", *reason)
			}
		},

		// ── DI-22: shares die with their resource; page type is fixed ────
		"DI22_DeletedPageSharesDoNotRegrantOnRecreate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			p := createPage(t, h, h.UserA.ID, "Shared")
			sharePage(t, h, p.ID, h.UserA.ID, h.UserB.ID, "viewer")
			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/pages/"+p.ID.String(), nil, h.UserA.ID).Code)

			// Alice re-creates a page with the same id (PUT takes it from the URL).
			w := h.Do(t, "PUT", "/api/v1/pages/"+p.ID.String(),
				map[string]interface{}{"title": "Private again", "type": "page", "isPrivate": true}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			w = h.Do(t, "GET", "/api/v1/pages/"+p.ID.String(), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, "the deleted page's share must not grant access to the new one")
		},

		"DI22_DeleteFolderRemovesSubtreeShares": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			pool := rawPool(t, h)
			f := createFolder(t, h, h.UserA.ID, "F")
			sub := createChild(t, h, h.UserA.ID, f.ID, "folder", "Sub")
			note := createChild(t, h, h.UserA.ID, sub.ID, "page", "Note")
			shareFolder(t, h, f.ID.String(), h.UserA.ID, h.UserB.ID, "viewer")
			shareFolder(t, h, sub.ID.String(), h.UserA.ID, h.UserB.ID, "editor")
			sharePage(t, h, note.ID, h.UserA.ID, h.UserB.ID, "viewer")

			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/pages/"+f.ID.String(), nil, h.UserA.ID).Code)

			var n int
			require.NoError(t, pool.QueryRow(context.Background(),
				`SELECT COUNT(*) FROM shares WHERE resource_id = ANY($1)`, []uuid.UUID{f.ID, sub.ID, note.ID},
			).Scan(&n))
			assert.Zero(t, n, "shares on the deleted subtree must be deleted with it")
		},

		"DI22_DeletedTemplateSharesDoNotRegrantOnRecreate": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/templates", map[string]interface{}{"name": "T"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			tmplID := Decode[struct{ ID string }](t, w).ID
			w = h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "template", "resourceId": tmplID,
				"sharedWithId": h.UserB.ID.String(), "permission": "viewer",
			}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/templates/"+tmplID, nil, h.UserA.ID).Code)

			w = h.Do(t, "PUT", "/api/v1/templates/"+tmplID, map[string]interface{}{"name": "T again", "isPrivate": true}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			w = h.Do(t, "GET", "/api/v1/templates/"+tmplID, nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, "the deleted template's share must not grant access to the new one")
		},

		// Changing page↔folder made existing shares unmanageable (the share
		// handler matches on type), so the type is fixed at creation.
		"DI22_PageTypeCannotChange": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			p := createPage(t, h, h.UserA.ID, "Page")

			w := h.Do(t, "PATCH", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{"type": "folder"}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			w = h.Do(t, "PUT", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{"title": "Page", "type": "folder"}, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Equal(t, model.NodeTypePage, getPage(t, h, h.UserA.ID, p.ID).Type)

			// Sending the current type is fine.
			w = h.Do(t, "PATCH", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{"type": "page", "title": "Renamed"}, h.UserA.ID)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		},

		// A second share for the same resource and recipient is a conflict,
		// not a 500 (Postgres unique violation) or a silent duplicate
		// (memstore).
		"DI22_DuplicateShareIsConflict": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			p := createPage(t, h, h.UserA.ID, "Shared")
			sharePage(t, h, p.ID, h.UserA.ID, h.UserB.ID, "viewer")
			w := h.Do(t, "POST", "/api/v1/shares", map[string]interface{}{
				"resourceType": "page", "resourceId": p.ID.String(),
				"sharedWithId": h.UserB.ID.String(), "permission": "editor",
			}, h.UserA.ID)
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		},

		// Kept history must not leak to whoever re-creates a page under the
		// deleted page's id (PUT takes a client-chosen id).
		"DI02_RecreatedPageIDDoesNotSeeOldHistory": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			p := createPage(t, h, h.UserA.ID, "Secret")
			_, pc := putContent(t, h, p.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("secret")})
			code, _ := putContent(t, h, p.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("v2"), "expectedRevision": pc.Revision})
			require.Equal(t, http.StatusOK, code)
			w := h.Do(t, "GET", "/api/v1/pages/"+p.ID.String()+"/content/versions", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			old := Decode[[]model.PageContentVersion](t, w)
			require.Len(t, old, 1)

			require.Equal(t, http.StatusNoContent, h.Do(t, "DELETE", "/api/v1/pages/"+p.ID.String(), nil, h.UserA.ID).Code)
			time.Sleep(5 * time.Millisecond) // created_at strictly after the archived versions

			w = h.Do(t, "PUT", "/api/v1/pages/"+p.ID.String(), map[string]interface{}{"title": "Mine now", "type": "page"}, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			w = h.Do(t, "GET", "/api/v1/pages/"+p.ID.String()+"/content/versions", nil, h.UserB.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Empty(t, Decode[[]model.PageContentVersion](t, w))
			w = h.Do(t, "POST", fmt.Sprintf("/api/v1/pages/%s/content/versions/%d/restore", p.ID, old[0].ID), nil, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		},
	})
}

func getPage(t *testing.T, h *Harness, userID, id uuid.UUID) model.Page {
	t.Helper()
	w := h.Do(t, "GET", "/api/v1/pages/"+id.String(), nil, userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return Decode[model.Page](t, w)
}

func createChild(t *testing.T, h *Harness, userID, parentID uuid.UUID, typ, title string) model.Page {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/pages",
		map[string]interface{}{"title": title, "type": typ, "parentId": parentID.String()}, userID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return Decode[model.Page](t, w)
}
