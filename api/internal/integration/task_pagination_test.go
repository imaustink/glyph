package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskPagination pins task list paging to a total order. Tasks share
// "order" values freely (default 0, Date.now collisions), and ordering by
// "order" alone lets OFFSET paging skip or repeat rows.
func TestTaskPagination(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"PaginationIsStableAcrossOrderTies": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			const n = 30
			for i := 0; i < n; i++ {
				w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": fmt.Sprintf("T%d", i)}, h.UserA.ID) // all order 0
				require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			}
			seen := map[uuid.UUID]int{}
			for offset := 0; offset < n; offset += 4 {
				w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks?limit=4&offset=%d", offset), nil, h.UserA.ID)
				require.Equal(t, http.StatusOK, w.Code)
				for _, task := range Decode[[]model.Task](t, w) {
					seen[task.ID]++
				}
			}
			assert.Len(t, seen, n, "paging through every offset must return every task exactly once")
			for id, c := range seen {
				assert.Equal(t, 1, c, "task %s returned %d times", id, c)
			}
		},
	})
}
