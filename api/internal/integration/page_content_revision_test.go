package integration

import (
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createPage(t *testing.T, h *Harness, userID uuid.UUID, title string) model.Page {
	t.Helper()
	w := h.Do(t, "POST", "/api/v1/pages", map[string]interface{}{"title": title}, userID)
	require.Equal(t, http.StatusCreated, w.Code)
	return Decode[model.Page](t, w)
}

func putContent(t *testing.T, h *Harness, pageID string, userID uuid.UUID, body map[string]interface{}) (int, model.PageContent) {
	t.Helper()
	w := h.Do(t, "PUT", "/api/v1/pages/"+pageID+"/content", body, userID)
	if w.Code != http.StatusOK {
		return w.Code, model.PageContent{}
	}
	return w.Code, Decode[model.PageContent](t, w)
}

func doc(text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "doc",
		"content": []map[string]interface{}{
			{"type": "paragraph", "content": []map[string]interface{}{
				{"type": "text", "text": text},
			}},
		},
	}
}

// TestPageContentRevision covers the optimistic-concurrency contract added to
// prevent a stale client silently overwriting newer content — the mechanism
// behind the 2026-09-21 production data-loss incidents.
func TestPageContentRevision(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"RevisionIncrementsOnEveryWrite": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Notes")

			code, c1 := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("one")})
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, 1, c1.Revision)

			code, c2 := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("two")})
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, 2, c2.Revision)
		},

		"GetContentReturnsCurrentRevision": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Notes")
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("one")})

			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			got := Decode[model.PageContent](t, w)
			assert.Equal(t, 1, got.Revision)
		},

		// The core guarantee: a write based on a stale read is rejected.
		"StaleExpectedRevisionIsRejectedWith409": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Notes")

			_, first := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("one")})
			staleRevision := first.Revision

			// Another writer advances the content.
			_, second := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{
				"content": doc("two"), "expectedRevision": staleRevision,
			})
			require.Equal(t, staleRevision+1, second.Revision)

			// The stale client now writes using the revision it last saw.
			w := h.Do(t, "PUT", "/api/v1/pages/"+page.ID.String()+"/content",
				map[string]interface{}{"content": doc("STALE CLOBBER"), "expectedRevision": staleRevision},
				h.UserA.ID)
			require.Equal(t, http.StatusConflict, w.Code, "stale write must be rejected")

			// Content is untouched.
			w = h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			got := Decode[model.PageContent](t, w)
			assert.Contains(t, string(got.Content), "two")
			assert.NotContains(t, string(got.Content), "STALE CLOBBER")
			assert.Equal(t, second.Revision, got.Revision)
		},

		"MatchingExpectedRevisionSucceeds": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Notes")
			_, first := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("one")})

			code, updated := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{
				"content": doc("two"), "expectedRevision": first.Revision,
			})
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, first.Revision+1, updated.Revision)
		},

		// Legacy clients that send no precondition must keep working.
		"OmittedExpectedRevisionStillWrites": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Notes")
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("one")})

			code, updated := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("two")})
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, 2, updated.Revision)
		},

		// History is what makes an overwrite recoverable without a PITR.
		"SupersededRevisionsAreRetrievable": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Notes")
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("first")})
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("second")})
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("third")})

			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content/versions", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			versions := Decode[[]model.PageContentVersion](t, w)
			require.Len(t, versions, 2, "two superseded revisions expected")

			// Newest first.
			assert.Contains(t, string(versions[0].Content), "second")
			assert.Contains(t, string(versions[1].Content), "first")
		},

		"VersionsAreNotReadableByAnUnrelatedUser": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Private")
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("secret")})
			putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{"content": doc("secret two")})

			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content/versions", nil, h.UserB.ID)
			assert.NotEqual(t, http.StatusOK, w.Code, "user B must not read user A's content history")
		},
	})
}

// TestPageParentValidation covers the cross-user cascade vector: parent_id was
// copied from the request with no validation, and pages.parent_id is
// ON DELETE CASCADE.
func TestPageParentValidation(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"CannotCreateUnderAnotherUsersPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			victim := createPage(t, h, h.UserA.ID, "Alice's folder")

			w := h.Do(t, "POST", "/api/v1/pages",
				map[string]interface{}{"title": "graft", "parentId": victim.ID.String()},
				h.UserB.ID)
			assert.NotEqual(t, http.StatusCreated, w.Code,
				"user B must not parent a page under user A's page")
		},

		"CannotUpsertUnderAnotherUsersPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			victim := createPage(t, h, h.UserA.ID, "Alice's folder")
			own := createPage(t, h, h.UserB.ID, "Bob's page")

			w := h.Do(t, "PUT", "/api/v1/pages/"+own.ID.String(),
				map[string]interface{}{"title": "moved", "parentId": victim.ID.String()},
				h.UserB.ID)
			assert.NotEqual(t, http.StatusOK, w.Code,
				"user B must not reparent under user A's page via upsert")
		},

		"CannotReparentUnderAnotherUsersPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			victim := createPage(t, h, h.UserA.ID, "Alice's folder")
			own := createPage(t, h, h.UserB.ID, "Bob's page")

			w := h.Do(t, "PATCH", "/api/v1/pages/"+own.ID.String(),
				map[string]interface{}{"parentId": victim.ID.String()}, h.UserB.ID)
			assert.NotEqual(t, http.StatusOK, w.Code,
				"user B must not reparent under user A's page via patch")
		},

		"CanStillNestUnderOwnPage": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			parent := createPage(t, h, h.UserA.ID, "My folder")

			w := h.Do(t, "POST", "/api/v1/pages",
				map[string]interface{}{"title": "child", "parentId": parent.ID.String()},
				h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			child := Decode[model.Page](t, w)
			require.NotNil(t, child.ParentID)
			assert.Equal(t, parent.ID, *child.ParentID)
		},

		"UnknownParentIsRejected": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/pages",
				map[string]interface{}{"title": "orphan", "parentId": "11111111-1111-1111-1111-111111111111"},
				h.UserA.ID)
			assert.NotEqual(t, http.StatusCreated, w.Code)
		},
	})
}
