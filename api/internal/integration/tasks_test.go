package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestTask(t *testing.T, h *Harness, userID uuid.UUID, title string) model.Task {
	t.Helper()
	body := map[string]interface{}{"title": title}
	w := h.Do(t, "POST", "/api/v1/tasks", body, userID)
	require.Equal(t, http.StatusCreated, w.Code)
	return Decode[model.Task](t, w)
}

func TestTasks(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		// ── Create ────────────────────────────────────────────────────────
		"CreateTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{"title": "My First Task"}
			w := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			task := Decode[model.Task](t, w)
			assert.Equal(t, "My First Task", task.Title)
			assert.Equal(t, h.UserA.ID, task.UserID)
			assert.NotEqual(t, uuid.Nil, task.ID)
			assert.False(t, task.CreatedAt.IsZero())
			assert.False(t, task.UpdatedAt.IsZero())
		},

		"CreateTaskDefaults": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Default Task"}, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			task := Decode[model.Task](t, w)
			assert.Equal(t, model.StatusTodo, task.Status)
			assert.Equal(t, model.PriorityNone, task.Priority)
			assert.Equal(t, []string{}, task.Tags)
			assert.Equal(t, "", task.Description)
			assert.Nil(t, task.DueDate)
			assert.Nil(t, task.SourcePageID)
			assert.Nil(t, task.SourceNodeID)
			assert.Equal(t, 0, task.Order)
		},

		"CreateTaskAllFields": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			body := map[string]interface{}{
				"title": "Full Task", "description": "A comprehensive task",
				"status": "in-progress", "priority": "high",
				"tags": []string{"urgent", "work"}, "order": 3,
			}
			w := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)

			task := Decode[model.Task](t, w)
			assert.Equal(t, "Full Task", task.Title)
			assert.Equal(t, "A comprehensive task", task.Description)
			assert.Equal(t, model.StatusInProgress, task.Status)
			assert.Equal(t, model.PriorityHigh, task.Priority)
			assert.Equal(t, []string{"urgent", "work"}, task.Tags)
			assert.Equal(t, 3, task.Order)
		},

		"CreateTaskWithSourceLink": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createTestPage(t, h, h.UserA.ID, "Source Page")
			nodeID := "node-abc-123"
			body := map[string]interface{}{
				"title": "Linked Task", "sourcePageId": page.ID.String(), "sourceNodeId": nodeID,
			}
			w := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			task := Decode[model.Task](t, w)
			require.NotNil(t, task.SourcePageID)
			assert.Equal(t, page.ID, *task.SourcePageID)
			require.NotNil(t, task.SourceNodeID)
			assert.Equal(t, nodeID, *task.SourceNodeID)
		},

		"CreateTaskAllStatuses": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for _, status := range []model.TaskStatus{
				model.StatusTodo, model.StatusInProgress, model.StatusDone, model.StatusCancelled,
			} {
				body := map[string]interface{}{"title": fmt.Sprintf("Task %s", status), "status": string(status)}
				w := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
				require.Equal(t, http.StatusCreated, w.Code)
				assert.Equal(t, status, Decode[model.Task](t, w).Status)
			}
		},

		"CreateTaskAllPriorities": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for _, p := range []model.Priority{
				model.PriorityUrgent, model.PriorityHigh, model.PriorityMedium, model.PriorityLow, model.PriorityNone,
			} {
				body := map[string]interface{}{"title": fmt.Sprintf("Task %s", p), "priority": string(p)}
				w := h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
				require.Equal(t, http.StatusCreated, w.Code)
				assert.Equal(t, p, Decode[model.Task](t, w).Priority)
			}
		},

		// ── List ──────────────────────────────────────────────────────────
		"ListTasksEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Task](t, w), 0)
		},

		"ListTasks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTask(t, h, h.UserA.ID, "Task 1")
			createTestTask(t, h, h.UserA.ID, "Task 2")
			createTestTask(t, h, h.UserA.ID, "Task 3")
			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Task](t, w), 3)
		},

		"ListTasksOrdering": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			for i, title := range []string{"Third", "First", "Second"} {
				orders := []int{3, 1, 2}
				body := map[string]interface{}{"title": title, "order": orders[i]}
				h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			}
			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID)
			tasks := Decode[[]model.Task](t, w)
			require.Len(t, tasks, 3)
			assert.Equal(t, "First", tasks[0].Title)
			assert.Equal(t, "Second", tasks[1].Title)
			assert.Equal(t, "Third", tasks[2].Title)
		},

		// ── Get ───────────────────────────────────────────────────────────
		"GetTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Get Me")
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks/%s", created.ID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			task := Decode[model.Task](t, w)
			assert.Equal(t, created.ID, task.ID)
			assert.Equal(t, "Get Me", task.Title)
		},

		"GetTaskNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks/%s", uuid.New()), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Update ────────────────────────────────────────────────────────
		"UpdateTaskStatus": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Update Status")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/tasks/%s", created.ID),
				map[string]interface{}{"status": "done"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			task := Decode[model.Task](t, w)
			assert.Equal(t, model.StatusDone, task.Status)
			assert.Equal(t, "Update Status", task.Title)
		},

		"UpdateTaskPriority": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Update Priority")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/tasks/%s", created.ID),
				map[string]interface{}{"priority": "urgent"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, model.PriorityUrgent, Decode[model.Task](t, w).Priority)
		},

		"UpdateTaskTags": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Tag Task")
			assert.Equal(t, []string{}, created.Tags)
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/tasks/%s", created.ID),
				map[string]interface{}{"tags": []string{"updated", "new-tag"}}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, []string{"updated", "new-tag"}, Decode[model.Task](t, w).Tags)
		},

		"UpdateTaskDescription": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Describe Me")
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/tasks/%s", created.ID),
				map[string]interface{}{"description": "Now I have a description"}, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "Now I have a description", Decode[model.Task](t, w).Description)
		},

		"UpdateTaskNotFound": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "PATCH", fmt.Sprintf("/api/v1/tasks/%s", uuid.New()),
				map[string]interface{}{"title": "Nope"}, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── Delete ────────────────────────────────────────────────────────
		"DeleteTask": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Delete Me")
			w := h.Do(t, "DELETE", fmt.Sprintf("/api/v1/tasks/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNoContent, w.Code)
			w = h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks/%s", created.ID), nil, h.UserA.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		"DeleteTaskReducesList": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			t1 := createTestTask(t, h, h.UserA.ID, "Task 1")
			createTestTask(t, h, h.UserA.ID, "Task 2")
			w := h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID)
			assert.Len(t, Decode[[]model.Task](t, w), 2)
			h.Do(t, "DELETE", fmt.Sprintf("/api/v1/tasks/%s", t1.ID), nil, h.UserA.ID)
			w = h.Do(t, "GET", "/api/v1/tasks", nil, h.UserA.ID)
			tasks := Decode[[]model.Task](t, w)
			assert.Len(t, tasks, 1)
			assert.Equal(t, "Task 2", tasks[0].Title)
		},

		// ── Upsert ────────────────────────────────────────────────────────
		"UpsertTaskCreatesWhenNew": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			id := uuid.New()
			body := map[string]interface{}{"title": "Upserted Task"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/tasks/%s", id), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			task := Decode[model.Task](t, w)
			assert.Equal(t, id, task.ID)
			assert.Equal(t, "Upserted Task", task.Title)
			assert.Equal(t, h.UserA.ID, task.UserID)
		},

		"UpsertTaskUpdatesWhenExists": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "Original")
			body := map[string]interface{}{"title": "Updated via Upsert", "status": "done"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/tasks/%s", created.ID), body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			task := Decode[model.Task](t, w)
			assert.Equal(t, "Updated via Upsert", task.Title)
			assert.Equal(t, model.StatusDone, task.Status)
		},

		"UpsertTaskDifferentUserGets404": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			created := createTestTask(t, h, h.UserA.ID, "UserA Task")
			body := map[string]interface{}{"title": "Hijack"}
			w := h.Do(t, "PUT", fmt.Sprintf("/api/v1/tasks/%s", created.ID), body, h.UserB.ID)
			assert.Equal(t, http.StatusNotFound, w.Code)
		},

		// ── ListBySourceNode ──────────────────────────────────────────────
		"ListTasksBySourceNode": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			nodeID := "node-abc-123"
			body := map[string]interface{}{"title": "Linked Task", "sourceNodeId": nodeID}
			h.Do(t, "POST", "/api/v1/tasks", body, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Other Task"}, h.UserA.ID)
			w := h.Do(t, "GET", fmt.Sprintf("/api/v1/tasks?sourceNodeId=%s", nodeID), nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			require.Len(t, tasks, 1)
			assert.Equal(t, "Linked Task", tasks[0].Title)
			require.NotNil(t, tasks[0].SourceNodeID)
			assert.Equal(t, nodeID, *tasks[0].SourceNodeID)
		},

		"ListTasksBySourceNodeEmpty": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			w := h.Do(t, "GET", "/api/v1/tasks?sourceNodeId=nonexistent-node", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Len(t, Decode[[]model.Task](t, w), 0)
		},

		// ── FilterTasks ───────────────────────────────────────────────────
		"FilterTasksEmptyRulesReturnsAll": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			createTestTask(t, h, h.UserA.ID, "Task 1")
			createTestTask(t, h, h.UserA.ID, "Task 2")
			body := map[string]interface{}{"conjunction": "and", "rules": []interface{}{}}
			w := h.Do(t, "POST", "/api/v1/tasks/filter", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			assert.Len(t, tasks, 2)
		},

		"FilterTasksByStatusEq": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Todo Task", "status": "todo"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Done Task", "status": "done"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "In Progress Task", "status": "in-progress"}, h.UserA.ID)
			body := map[string]interface{}{
				"conjunction": "and",
				"rules": []interface{}{
					map[string]interface{}{"id": "r1", "field": "status", "operator": "eq", "value": "done"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/tasks/filter", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			require.Len(t, tasks, 1)
			assert.Equal(t, "Done Task", tasks[0].Title)
		},

		"FilterTasksByPriorityEq": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "High Task", "priority": "high"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Low Task", "priority": "low"}, h.UserA.ID)
			body := map[string]interface{}{
				"conjunction": "and",
				"rules": []interface{}{
					map[string]interface{}{"id": "r1", "field": "priority", "operator": "eq", "value": "high"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/tasks/filter", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			require.Len(t, tasks, 1)
			assert.Equal(t, "High Task", tasks[0].Title)
		},

		"FilterTasksByStatusIn": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Todo Task", "status": "todo"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Done Task", "status": "done"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Cancelled Task", "status": "cancelled"}, h.UserA.ID)
			body := map[string]interface{}{
				"conjunction": "and",
				"rules": []interface{}{
					map[string]interface{}{"id": "r1", "field": "status", "operator": "in", "value": []string{"done", "cancelled"}},
				},
			}
			w := h.Do(t, "POST", "/api/v1/tasks/filter", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			assert.Len(t, tasks, 2)
		},

		"FilterTasksConjunctionOr": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Urgent High", "priority": "urgent", "status": "todo"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Low Done", "priority": "low", "status": "done"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "Low Todo", "priority": "low", "status": "todo"}, h.UserA.ID)
			body := map[string]interface{}{
				"conjunction": "or",
				"rules": []interface{}{
					map[string]interface{}{"id": "r1", "field": "priority", "operator": "eq", "value": "urgent"},
					map[string]interface{}{"id": "r2", "field": "status", "operator": "eq", "value": "done"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/tasks/filter", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			assert.Len(t, tasks, 2)
		},

		"FilterTasksIsolatedByUser": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "UserA Done", "status": "done"}, h.UserA.ID)
			h.Do(t, "POST", "/api/v1/tasks", map[string]interface{}{"title": "UserB Done", "status": "done"}, h.UserB.ID)
			body := map[string]interface{}{
				"conjunction": "and",
				"rules": []interface{}{
					map[string]interface{}{"id": "r1", "field": "status", "operator": "eq", "value": "done"},
				},
			}
			w := h.Do(t, "POST", "/api/v1/tasks/filter", body, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code)
			tasks := Decode[[]model.Task](t, w)
			require.Len(t, tasks, 1)
			assert.Equal(t, "UserA Done", tasks[0].Title)
		},

		"FilterTasksInvalidBodyReturns400": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			req := map[string]interface{}{"conjunction": 123} // invalid type
			w := h.Do(t, "POST", "/api/v1/tasks/filter", req, h.UserA.ID)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		},
	})
}
