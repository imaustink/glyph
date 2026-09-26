package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
	})
}
