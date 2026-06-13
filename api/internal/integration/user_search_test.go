package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserSearch(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"SearchReturnsMatchingUsers": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search?q=bob", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			require.Len(t, results, 1)
			assert.Equal(t, h.UserB.ID.String(), results[0]["id"])
		},
		"SearchExcludesCurrentUser": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search?q=alice", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, results)
		},
		"EmptyQueryReturnsEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			assert.Empty(t, results)
		},
		"SearchByEmail": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search?q=bob%40test.com", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			require.Len(t, results, 1)
			assert.Equal(t, h.UserB.ID.String(), results[0]["id"])
		},
		"SearchIsCaseInsensitive": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/users/search?q=BOB", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			results := Decode[[]map[string]interface{}](t, w)
			require.Len(t, results, 1)
			assert.Equal(t, h.UserB.ID.String(), results[0]["id"])
		},
	})
}
