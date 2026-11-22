package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnippet(t *testing.T) {
	long := strings.Repeat("a ", 100) + "needle" + strings.Repeat(" b", 100)
	s := snippet(long, "NEEDLE")
	assert.Contains(t, s, "needle")
	assert.True(t, strings.HasPrefix(s, "…"))
	assert.True(t, strings.HasSuffix(s, "…"))

	assert.Equal(t, "short text", snippet("short\n\ntext", "zzz"))

	// Runes whose lower-case form is longer must not misalign indexes.
	s = snippet(strings.Repeat("İ", 200)+" target", "target")
	assert.Contains(t, s, "target")
}

// searchPagesFixture returns n pages whose titles contain "meeting" (the first
// of which also matches by content) plus one page that matches by content only.
func searchPagesFixture(n int) ([]*model.Page, *mockPageStore) {
	pages := make([]*model.Page, 0, n+1)
	for i := 0; i < n; i++ {
		pages = append(pages, &model.Page{ID: uuid.New(), Type: model.NodeTypePage, Title: fmt.Sprintf("Meeting %d", i)})
	}
	bodyOnly := &model.Page{ID: uuid.New(), Type: model.NodeTypePage, Title: "Weekly sync"}
	pages = append(pages, bodyOnly)
	bothID := pages[0].ID
	ps := &mockPageStore{
		listByUserFn: func(context.Context, uuid.UUID) ([]*model.Page, error) { return pages, nil },
		searchContentFn: func(ids []uuid.UUID, _ string, limit int) ([]store.PageTextMatch, error) {
			out := []store.PageTextMatch{}
			for _, id := range ids {
				if (id == bothID || id == bodyOnly.ID) && len(out) < limit {
					out = append(out, store.PageTextMatch{PageID: id, Text: "notes from the meeting about the roadmap"})
				}
			}
			return out, nil
		},
	}
	return pages, ps
}

func doSearch(t *testing.T, h *SearchHandler, query string) (int, []searchResult) {
	t.Helper()
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/search", h.Search)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/search?"+query, nil))
	var out []searchResult
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return w.Code, out
}

func TestSearch_DefaultTitleHitsCrowdOutContent(t *testing.T) {
	// Existing API/MCP behaviour is unchanged: title hits come first and
	// fill the limit.
	_, ps := searchPagesFixture(5)
	code, res := doSearch(t, &SearchHandler{Pages: ps, Tasks: &mockTaskStore{}}, "q=meeting&types=page&limit=5")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, res, 5)
	for _, r := range res {
		assert.Empty(t, r.Snippet)
	}
}

func TestSearch_MatchContentSkipsTitles(t *testing.T) {
	pages, ps := searchPagesFixture(5)
	code, res := doSearch(t, &SearchHandler{Pages: ps, Tasks: &mockTaskStore{}}, "q=meeting&types=page&limit=5&match=content")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, res, 2)
	ids := []uuid.UUID{res[0].ID, res[1].ID}
	// A page whose title and body both match still gets a snippet.
	assert.Contains(t, ids, pages[0].ID)
	assert.Contains(t, ids, pages[len(pages)-1].ID)
	for _, r := range res {
		assert.Contains(t, r.Snippet, "meeting")
	}
}

func TestSearch_InvalidMatch(t *testing.T) {
	_, ps := searchPagesFixture(1)
	code, _ := doSearch(t, &SearchHandler{Pages: ps, Tasks: &mockTaskStore{}}, "q=x&match=title")
	assert.Equal(t, http.StatusBadRequest, code)
}
