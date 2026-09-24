package handler

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// SearchHandler serves server-side search across pages and tasks. The web
// app searches client-side (Fuse.js over what it has loaded); this endpoint
// exists for API and MCP clients that don't hold the whole workspace.
type SearchHandler struct {
	Pages store.PageStore
	Tasks store.TaskStore
}

type searchResult struct {
	ID       uuid.UUID  `json:"id"`
	Type     string     `json:"type"` // "page" | "folder" | "task"
	Title    string     `json:"title"`
	Snippet  string     `json:"snippet"`
	Status   string     `json:"status,omitempty"`
	ParentID *uuid.UUID `json:"parentId,omitempty"`
}

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 100
	snippetRadius      = 80
)

// GET /search?q=…&types=page,task&limit=20
//
// Case-insensitive substring match on page titles and content, and on task
// titles and descriptions. Results are title matches first, then content
// matches, each in the store's recency order.
func (h *SearchHandler) Search(c *gin.Context) {
	user := auth.CurrentUser(c)
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
		return
	}
	if utf8.RuneCountInString(query) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is too long"})
		return
	}
	limit := defaultSearchLimit
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = min(n, maxSearchLimit)
	}
	wantPages, wantTasks := true, true
	if raw := c.Query("types"); raw != "" {
		wantPages, wantTasks = false, false
		for _, t := range strings.Split(raw, ",") {
			switch strings.TrimSpace(t) {
			case "page":
				wantPages = true
			case "task":
				wantTasks = true
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "types must be a comma-separated list of page, task"})
				return
			}
		}
	}

	// A bearer token without the read scope for a type simply gets no
	// results of that type, rather than a 403 for the whole search.
	scope := currentTokenScope(c)
	if scope != nil {
		wantPages = wantPages && hasReadScope(scope, model.ShareResourcePage)
		wantTasks = wantTasks && hasReadScope(scope, model.ShareResourceTask)
	}

	lq := strings.ToLower(query)
	var titleHits, contentHits []searchResult
	ctx := c.Request.Context()

	if wantPages {
		pages, err := h.Pages.ListByUser(ctx, user.ID)
		if err != nil {
			internalError(c, err)
			return
		}
		pages = FilterByTokenScope(c, model.ShareResourcePage, pages, func(p *model.Page) *uuid.UUID { return p.OrgID })
		byID := make(map[uuid.UUID]*model.Page, len(pages))
		var contentCandidates []uuid.UUID
		for _, p := range pages {
			byID[p.ID] = p
			if strings.Contains(strings.ToLower(p.Title), lq) {
				titleHits = append(titleHits, searchResult{ID: p.ID, Type: string(p.Type), Title: p.Title, ParentID: p.ParentID})
			} else if p.Type == model.NodeTypePage {
				contentCandidates = append(contentCandidates, p.ID)
			}
		}
		matches, err := h.Pages.SearchContent(ctx, user.ID, contentCandidates, query, limit)
		if err != nil {
			internalError(c, err)
			return
		}
		for _, m := range matches {
			p := byID[m.PageID]
			if p == nil {
				continue
			}
			contentHits = append(contentHits, searchResult{ID: p.ID, Type: string(p.Type), Title: p.Title, Snippet: snippet(m.Text, query), ParentID: p.ParentID})
		}
	}

	if wantTasks {
		tasks, err := h.Tasks.ListByUser(ctx, user.ID)
		if err != nil {
			internalError(c, err)
			return
		}
		tasks = FilterByTokenScope(c, model.ShareResourceTask, tasks, func(t *model.Task) *uuid.UUID { return t.OrgID })
		for _, t := range tasks {
			r := searchResult{ID: t.ID, Type: "task", Title: t.Title, Status: string(t.Status)}
			switch {
			case strings.Contains(strings.ToLower(t.Title), lq):
				titleHits = append(titleHits, r)
			case strings.Contains(strings.ToLower(t.Description), lq):
				r.Snippet = snippet(t.Description, query)
				contentHits = append(contentHits, r)
			}
		}
	}

	results := append(titleHits, contentHits...)
	if len(results) > limit {
		results = results[:limit]
	}
	if results == nil {
		results = []searchResult{}
	}
	c.JSON(http.StatusOK, results)
}

func hasReadScope(scope *model.TokenScope, rt model.ShareResourceType) bool {
	for _, s := range scope.Scopes {
		if t, ok := s.ResourceType(); ok && t == rt {
			return true
		}
	}
	return false
}

// snippet returns up to snippetRadius runes either side of the first
// case-insensitive occurrence of query in text, with ellipses where cut.
func snippet(text, query string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	// Lower-case rune by rune rather than with strings.ToLower, which can
	// change the rune count (e.g. 'İ') and would misalign indexes into runes.
	lower := lowerRunes(runes)
	q := lowerRunes([]rune(query))
	idx := -1
	for i := 0; i+len(q) <= len(lower); i++ {
		if string(lower[i:i+len(q)]) == string(q) {
			idx = i
			break
		}
	}
	if idx < 0 {
		if len(runes) > 2*snippetRadius {
			return string(runes[:2*snippetRadius]) + "…"
		}
		return text
	}
	start := max(0, idx-snippetRadius)
	end := min(len(runes), idx+len(q)+snippetRadius)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

func lowerRunes(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = unicode.ToLower(r)
	}
	return out
}
