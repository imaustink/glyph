package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// ─── parseUUID coverage ───────────────────────────────────────────────────────

func TestParseUUID_InvalidUUID_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/lanes/:id", h.GetLane)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/lanes/not-a-uuid", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid UUID: want 400, got %d", w.Code)
	}
}

// ─── LaneHandler – GetLane ────────────────────────────────────────────────────

func TestLaneHandler_GetLane_NotFound_Returns404(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/lanes/:id", h.GetLane)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/lanes/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetLane not found: want 404, got %d", w.Code)
	}
}

func TestLaneHandler_GetLane_Returns200(t *testing.T) {
	laneID := uuid.New()
	h := &LaneHandler{Lanes: &mockLaneStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Lane, error) {
			return &model.Lane{ID: id, Title: "My Lane"}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/lanes/:id", h.GetLane)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/lanes/"+laneID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetLane happy: want 200, got %d", w.Code)
	}
}

// ─── LaneHandler – UpdateLane ─────────────────────────────────────────────────

func TestLaneHandler_UpdateLane_NotFound_Returns404(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/lanes/:id", h.UpdateLane)

	body := jsonBody(t, map[string]string{"title": "New"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/lanes/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpdateLane not found: want 404, got %d", w.Code)
	}
}

func TestLaneHandler_UpdateLane_Returns200(t *testing.T) {
	laneID := uuid.New()
	h := &LaneHandler{Lanes: &mockLaneStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Lane, error) {
			return &model.Lane{ID: id, Title: "Old"}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/lanes/:id", h.UpdateLane)

	title := "New Title"
	body := jsonBody(t, map[string]*string{"title": &title})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/lanes/"+laneID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpdateLane happy: want 200, got %d", w.Code)
	}
}

// ─── LaneHandler – DeleteLane ─────────────────────────────────────────────────

func TestLaneHandler_DeleteLane_Returns204(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/lanes/:id", h.DeleteLane)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/lanes/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("DeleteLane happy: want 204, got %d", w.Code)
	}
}

func TestLaneHandler_DeleteLane_StoreError_Returns404(t *testing.T) {
	// The laneDeleteErrStore is replaced with a simple mock that returns ErrNotFound from Delete.
	h := &LaneHandler{Lanes: &mockLaneStoreDeleteErr{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/lanes/:id", h.DeleteLane)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/lanes/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("DeleteLane not found: want 404, got %d", w.Code)
	}
}

// mockLaneStoreDeleteErr returns ErrNotFound from Delete; all other methods delegate to embedded mockLaneStore.
type mockLaneStoreDeleteErr struct{ mockLaneStore }

func (s *mockLaneStoreDeleteErr) Delete(_ context.Context, _, _ uuid.UUID) error {
	return store.ErrNotFound
}

// ─── LaneHandler – UpsertLane ─────────────────────────────────────────────────

func TestLaneHandler_UpsertLane_Returns200(t *testing.T) {
	laneID := uuid.New()
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/lanes/:id", h.UpsertLane)

	body := jsonBody(t, model.Lane{Title: "Upserted Lane"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/lanes/"+laneID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpsertLane happy: want 200, got %d", w.Code)
	}
}

func TestLaneHandler_UpsertLane_StoreError_Returns404(t *testing.T) {
	laneID := uuid.New()
	storeErr := errors.New("upsert error")
	h := &LaneHandler{Lanes: &mockLaneStore{
		upsertFn: func(l *model.Lane) (*model.Lane, error) { return nil, storeErr },
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/lanes/:id", h.UpsertLane)

	body := jsonBody(t, model.Lane{Title: "Lane"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/lanes/"+laneID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpsertLane store error: want 404, got %d", w.Code)
	}
}

// ─── PageHandler – GetPage ────────────────────────────────────────────────────

func TestPageHandler_GetPage_NotFound_Returns404(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages/:id", h.GetPage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetPage not found: want 404, got %d", w.Code)
	}
}

func TestPageHandler_GetPage_Returns200(t *testing.T) {
	callerID := testUser().ID
	h := &PageHandler{Pages: &mockPageStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
			return testPage(callerID), nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages/:id", h.GetPage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetPage happy: want 200, got %d", w.Code)
	}
}

// ─── PageHandler – UpdatePage ─────────────────────────────────────────────────

func TestPageHandler_UpdatePage_NotFound_Returns404(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/pages/:id", h.UpdatePage)

	body := jsonBody(t, map[string]string{"title": "New"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/pages/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpdatePage not found: want 404, got %d", w.Code)
	}
}

func TestPageHandler_UpdatePage_OwnerReturns200(t *testing.T) {
	callerID := testUser().ID
	h := &PageHandler{
		Pages: &mockPageStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
				return testPage(callerID), nil
			},
		},
		Perms: &PermissionChecker{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/pages/:id", h.UpdatePage)

	body := jsonBody(t, map[string]any{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/pages/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpdatePage owner: want 200, got %d", w.Code)
	}
}

// ─── PageHandler – UpsertPage ─────────────────────────────────────────────────

func TestPageHandler_UpsertPage_Returns200(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/pages/:id", h.UpsertPage)

	body := jsonBody(t, model.Page{Title: "Upserted"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/pages/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpsertPage happy: want 200, got %d", w.Code)
	}
}

// ─── PageHandler – GetPageContent ─────────────────────────────────────────────

func TestPageHandler_GetPageContent_NotFound_Returns404(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages/:id/content", h.GetPageContent)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages/"+uuid.New().String()+"/content", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetPageContent not found: want 404, got %d", w.Code)
	}
}

func TestPageHandler_GetPageContent_Returns200(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{
		getContentFn: func(pageID, _ uuid.UUID) (*model.PageContent, error) {
			return &model.PageContent{PageID: pageID}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages/:id/content", h.GetPageContent)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages/"+uuid.New().String()+"/content", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetPageContent happy: want 200, got %d", w.Code)
	}
}

// ─── PageHandler – ListPages pagination ───────────────────────────────────────

func TestPageHandler_ListPages_InvalidLimit_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages", h.ListPages)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages?limit=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListPages invalid limit: want 400, got %d", w.Code)
	}
}

func TestPageHandler_ListPages_InvalidOffset_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages", h.ListPages)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages?offset=-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListPages invalid offset: want 400, got %d", w.Code)
	}
}

func TestPageHandler_ListPages_Paginated_Returns200(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages", h.ListPages)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages?limit=10&offset=0", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ListPages paginated: want 200, got %d", w.Code)
	}
}

// ─── TaskHandler – GetTask ────────────────────────────────────────────────────

func TestTaskHandler_GetTask_NotFound_Returns404(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks/:id", h.GetTask)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetTask not found: want 404, got %d", w.Code)
	}
}

func TestTaskHandler_GetTask_Returns200(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &TaskHandler{Tasks: &mockTaskStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
			return &model.Task{ID: taskID, UserID: callerID, Title: "Task"}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks/:id", h.GetTask)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+taskID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetTask happy: want 200, got %d", w.Code)
	}
}

// ─── TaskHandler – UpdateTask ─────────────────────────────────────────────────

func TestTaskHandler_UpdateTask_NotFound_Returns404(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/tasks/:id", h.UpdateTask)

	body := jsonBody(t, map[string]string{"title": "New"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/tasks/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpdateTask not found: want 404, got %d", w.Code)
	}
}

func TestTaskHandler_UpdateTask_OwnerReturns200(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &TaskHandler{
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
		Perms: &PermissionChecker{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/tasks/:id", h.UpdateTask)

	body := jsonBody(t, map[string]any{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpdateTask owner: want 200, got %d", w.Code)
	}
}

// ─── TaskHandler – DeleteTask ─────────────────────────────────────────────────

func TestTaskHandler_DeleteTask_NotFound_Returns404(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/tasks/:id", h.DeleteTask)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/tasks/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("DeleteTask not found: want 404, got %d", w.Code)
	}
}

func TestTaskHandler_DeleteTask_NonOwner_Returns403(t *testing.T) {
	otherUserID := uuid.New()
	taskID := uuid.New()
	h := &TaskHandler{Tasks: &mockTaskStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
			return &model.Task{ID: taskID, UserID: otherUserID}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/tasks/:id", h.DeleteTask)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/tasks/"+taskID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("DeleteTask non-owner: want 403, got %d", w.Code)
	}
}

func TestTaskHandler_DeleteTask_OwnerReturns204(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &TaskHandler{Tasks: &mockTaskStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
			return &model.Task{ID: taskID, UserID: callerID}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/tasks/:id", h.DeleteTask)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/tasks/"+taskID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("DeleteTask owner: want 204, got %d", w.Code)
	}
}

// ─── TaskHandler – UpsertTask ─────────────────────────────────────────────────

func TestTaskHandler_UpsertTask_Returns200(t *testing.T) {
	taskID := uuid.New()
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/tasks/:id", h.UpsertTask)

	body := jsonBody(t, model.Task{Title: "Upserted"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/tasks/"+taskID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpsertTask happy: want 200, got %d", w.Code)
	}
}

// ─── TaskHandler – ListTasks coverage ────────────────────────────────────────

func TestTaskHandler_ListTasks_InvalidSourcePageId_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks", h.ListTasks)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks?sourcePageId=not-a-uuid", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListTasks invalid sourcePageId: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_ListTasks_InvalidLimit_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks", h.ListTasks)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks?limit=-5", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListTasks negative limit: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_ListTasks_Paginated_Returns200(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks", h.ListTasks)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks?limit=10&offset=0", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ListTasks paginated: want 200, got %d", w.Code)
	}
}

// ─── TaskHandler – FilterTasks happy path ────────────────────────────────────

func TestTaskHandler_FilterTasks_HappyPath_Returns200(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/tasks/filter", h.FilterTasks)

	body := jsonBody(t, model.FilterSet{Conjunction: "and", Rules: []model.FilterRule{}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks/filter", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("FilterTasks happy: want 200, got %d", w.Code)
	}
}

// ─── TemplateHandler – GetTemplate ───────────────────────────────────────────

func TestTemplateHandler_GetTemplate_NotFound_Returns404(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/templates/:id", h.GetTemplate)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/templates/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GetTemplate not found: want 404, got %d", w.Code)
	}
}

func TestTemplateHandler_GetTemplate_Returns200(t *testing.T) {
	callerID := testUser().ID
	h := &TemplateHandler{Templates: &mockTemplateStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Template, error) {
			return &model.Template{ID: id, UserID: callerID}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/templates/:id", h.GetTemplate)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/templates/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetTemplate happy: want 200, got %d", w.Code)
	}
}

// ─── TemplateHandler – UpdateTemplate ────────────────────────────────────────

func TestTemplateHandler_UpdateTemplate_NotFound_Returns404(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/templates/:id", h.UpdateTemplate)

	body := jsonBody(t, map[string]string{"name": "New"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/templates/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpdateTemplate not found: want 404, got %d", w.Code)
	}
}

func TestTemplateHandler_UpdateTemplate_Returns200(t *testing.T) {
	callerID := testUser().ID
	h := &TemplateHandler{Templates: &mockTemplateStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Template, error) {
			return &model.Template{ID: id, UserID: callerID, Name: "Old"}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/templates/:id", h.UpdateTemplate)

	body := jsonBody(t, map[string]any{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/templates/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpdateTemplate happy: want 200, got %d", w.Code)
	}
}

// ─── TemplateHandler – DeleteTemplate ────────────────────────────────────────

func TestTemplateHandler_DeleteTemplate_Returns204(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/templates/:id", h.DeleteTemplate)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/templates/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("DeleteTemplate happy: want 204, got %d", w.Code)
	}
}

// ─── TemplateHandler – UpsertTemplate ────────────────────────────────────────

func TestTemplateHandler_UpsertTemplate_Returns200(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/templates/:id", h.UpsertTemplate)

	body := jsonBody(t, model.Template{Name: "Upserted"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/templates/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("UpsertTemplate happy: want 200, got %d", w.Code)
	}
}

// ─── ShareHandler – ListShares ────────────────────────────────────────────────

func TestShareHandler_ListShares_MissingParams_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/shares", h.ListShares)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shares", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListShares missing params: want 400, got %d", w.Code)
	}
}

func TestShareHandler_ListShares_InvalidResourceType_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/shares", h.ListShares)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shares?resourceType=invalid&resourceId="+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListShares invalid resourceType: want 400, got %d", w.Code)
	}
}

func TestShareHandler_ListShares_InvalidResourceId_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/shares", h.ListShares)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shares?resourceType=task&resourceId=not-a-uuid", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListShares invalid resourceId: want 400, got %d", w.Code)
	}
}

func TestShareHandler_ListShares_ResourceNotFound_Returns404(t *testing.T) {
	// isResourceOwner: Tasks.GetByID returns ErrNotFound
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks:  &mockTaskStore{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/shares", h.ListShares)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shares?resourceType=task&resourceId="+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("ListShares resource not found: want 404, got %d", w.Code)
	}
}

func TestShareHandler_ListShares_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db error")
	callerID := testUser().ID
	taskID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{
			listForResourceFn: func(_ model.ShareResourceType, _ uuid.UUID) ([]*model.Share, error) {
				return nil, storeErr
			},
		},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/shares", h.ListShares)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shares?resourceType=task&resourceId="+taskID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListShares store error: want 500, got %d", w.Code)
	}
}

func TestShareHandler_ListShares_Returns200(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/shares", h.ListShares)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shares?resourceType=task&resourceId="+taskID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ListShares happy: want 200, got %d", w.Code)
	}
}

// ─── ShareHandler – CreateShare ───────────────────────────────────────────────

func TestShareHandler_CreateShare_MissingParams_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare missing params: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_NoSharedWith_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{"resourceType": "task", "resourceId": uuid.New().String()})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare no sharedWith: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_InvalidResourceType_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType":   "invalid",
		"resourceId":     uuid.New().String(),
		"sharedWithEmail": "other@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare invalid resourceType: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_InvalidResourceId_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType":    "task",
		"resourceId":      "not-a-uuid",
		"sharedWithEmail": "other@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare invalid resourceId: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_InvalidPermission_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType":    "task",
		"resourceId":      uuid.New().String(),
		"sharedWithEmail": "other@example.com",
		"permission":      "superadmin",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare invalid permission: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_ResourceNotFound_Returns404(t *testing.T) {
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks:  &mockTaskStore{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType":    "task",
		"resourceId":      uuid.New().String(),
		"sharedWithEmail": "other@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("CreateShare resource not found: want 404, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_InvalidSharedWithId_Returns400(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType": "task",
		"resourceId":   taskID.String(),
		"sharedWithId": "not-a-uuid",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare invalid sharedWithId: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_UserByIDNotFound_Returns404(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
		Users: &mockUserStore{
			getByIDFn: func(_ uuid.UUID) (*model.User, error) { return nil, store.ErrNotFound },
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType": "task",
		"resourceId":   taskID.String(),
		"sharedWithId": uuid.New().String(),
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("CreateShare user by ID not found: want 404, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_UserByEmailNotFound_Returns404(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
		Users: &mockUserStore{
			getByEmailFn: func(_ string) (*model.User, error) { return nil, store.ErrNotFound },
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType":    "task",
		"resourceId":      taskID.String(),
		"sharedWithEmail": "nobody@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("CreateShare user by email not found: want 404, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_SelfShare_Returns400(t *testing.T) {
	callerID := testUser().ID
	taskID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
		Users: &mockUserStore{
			getByIDFn: func(_ uuid.UUID) (*model.User, error) {
				return &model.User{ID: callerID}, nil
			},
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType": "task",
		"resourceId":   taskID.String(),
		"sharedWithId": callerID.String(),
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare self-share: want 400, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_StoreError_Returns500(t *testing.T) {
	callerID := testUser().ID
	otherID := uuid.New()
	taskID := uuid.New()
	storeErr := errors.New("create error")
	h := &ShareHandler{
		Shares: &mockShareStore{
			createShareFn: func(_ *model.Share) (*model.Share, error) { return nil, storeErr },
		},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: callerID}, nil
			},
		},
		Users: &mockUserStore{
			getByIDFn: func(_ uuid.UUID) (*model.User, error) {
				return &model.User{ID: otherID}, nil
			},
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType": "task",
		"resourceId":   taskID.String(),
		"sharedWithId": otherID.String(),
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreateShare store error: want 500, got %d", w.Code)
	}
}

func TestShareHandler_CreateShare_Returns201(t *testing.T) {
	callerID := testUser().ID
	otherID := uuid.New()
	pageID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Pages: &mockPageStore{
			getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
				return testPage(callerID), nil
			},
		},
		Users: &mockUserStore{
			getByEmailFn: func(_ string) (*model.User, error) {
				return &model.User{ID: otherID}, nil
			},
		},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	body := jsonBody(t, map[string]string{
		"resourceType":    "page",
		"resourceId":      pageID.String(),
		"sharedWithEmail": "other@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shares", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("CreateShare happy: want 201, got %d", w.Code)
	}
}

// ─── isResourceOwner – page not-found path ───────────────────────────────────

func TestShareHandler_isResourceOwner_Page_NotFound_Returns404(t *testing.T) {
	shareID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{
			getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
				return &model.Share{
					ResourceType: model.ShareResourcePage,
					ResourceID:   uuid.New(),
				}, nil
			},
		},
		Pages: &mockPageStore{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/shares/:shareId", h.DeleteShare)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+shareID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("isResourceOwner page not found: want 404, got %d", w.Code)
	}
}

// ─── OrgHandler – GetOrg happy path ──────────────────────────────────────────

func TestOrgHandler_GetOrg_Returns200(t *testing.T) {
	orgID := uuid.New()
	h := &OrgHandler{Orgs: &mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
		},
		getByIDFn: func(id uuid.UUID) (*model.Organization, error) {
			return &model.Organization{ID: id, Name: "My Org"}, nil
		},
		listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
			return []*model.OrgMember{}, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/orgs/:orgId", h.GetOrg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orgs/"+orgID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetOrg happy: want 200, got %d", w.Code)
	}
}

// ─── OrgHandler – DeleteOrg happy path ───────────────────────────────────────

func TestOrgHandler_DeleteOrg_Returns204(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore(nil)}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId", h.DeleteOrg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("DeleteOrg happy: want 204, got %d", w.Code)
	}
}

// ─── OrgHandler – UpdateOrg GetByID error ────────────────────────────────────

// ─── OrgHandler – GetOrg GetByID/ListMembers error paths ─────────────────────

func TestOrgHandler_GetOrg_GetByIDError_Returns404(t *testing.T) {
orgID := uuid.New()
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
},
getByIDFn: func(_ uuid.UUID) (*model.Organization, error) {
return nil, store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/orgs/:orgId", h.GetOrg)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/orgs/"+orgID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("GetOrg GetByID error: want 404, got %d", w.Code)
}
}
func TestOrgHandler_ListOrgs_Success(t *testing.T) {
h := &OrgHandler{Orgs: &mockOrgStore{
listForUserFn: func(_ uuid.UUID) ([]*model.OrgWithRole, error) {
return []*model.OrgWithRole{}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/orgs", h.ListOrgs)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/orgs", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("ListOrgs success: want 200, got %d", w.Code)
}
}

func TestOrgHandler_ListOrgs_Error_Returns500(t *testing.T) {
h := &OrgHandler{Orgs: &mockOrgStore{
listForUserFn: func(_ uuid.UUID) ([]*model.OrgWithRole, error) {
return nil, errors.New("list error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/orgs", h.ListOrgs)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/orgs", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("ListOrgs error: want 500, got %d", w.Code)
}
}

// ─── OrgHandler – UpdateOrg paths ────────────────────────────────────────────

func TestOrgHandler_UpdateOrg_GetByIDError_Returns404(t *testing.T) {
orgID := uuid.New()
h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
getByIDFn: func(_ uuid.UUID) (*model.Organization, error) {
return nil, store.ErrNotFound
},
})}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId", h.UpdateOrg)
body := jsonBody(t, map[string]any{"name": "New Name"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/orgs/"+orgID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("UpdateOrg GetByID error: want 404, got %d", w.Code)
}
}

// ─── OrgHandler – orgOwnerGuard non-owner ────────────────────────────────────

func TestOrgHandler_orgOwnerGuard_NonOwner_Returns403(t *testing.T) {
orgID := uuid.New()
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId", h.DeleteOrg)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+orgID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusForbidden {
t.Errorf("orgOwnerGuard non-owner: want 403, got %d", w.Code)
}
}

// ─── OrgHandler – RemoveOrgMember paths ──────────────────────────────────────

func TestOrgHandler_RemoveOrgMember_Success_Returns204(t *testing.T) {
orgID := uuid.New()
memberID := uuid.New()
callerID := testUser().ID
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
// orgOwnerGuard calls with callerID; subsequent call uses memberID
if userID == callerID {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
}
return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+orgID.String()+"/members/"+memberID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNoContent {
t.Errorf("RemoveOrgMember success: want 204, got %d", w.Code)
}
}

func TestOrgHandler_RemoveOrgMember_RemoveError_Returns500(t *testing.T) {
orgID := uuid.New()
memberID := uuid.New()
callerID := testUser().ID
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
if userID == callerID {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
}
return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
},
removeMemberFn: func(_, _ uuid.UUID) error {
return errors.New("remove error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+orgID.String()+"/members/"+memberID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("RemoveOrgMember remove error: want 500, got %d", w.Code)
}
}

// ─── LaneHandler – UpdateLane UpdateError ────────────────────────────────────

func TestLaneHandler_UpdateLane_UpdateError_Returns500(t *testing.T) {
laneID := uuid.New()
callerID := testUser().ID
h := &LaneHandler{Lanes: &mockLaneStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Lane, error) {
return &model.Lane{ID: laneID, UserID: callerID}, nil
},
updateFn: func(_ *model.Lane) (*model.Lane, error) {
return nil, errors.New("update error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/lanes/:id", h.UpdateLane)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/lanes/"+laneID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("UpdateLane update error: want 500, got %d", w.Code)
}
}

// ─── LaneHandler – ListLanes success / CreateLane success ────────────────────

func TestLaneHandler_ListLanes_Success(t *testing.T) {
h := &LaneHandler{Lanes: &mockLaneStore{
listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Lane, error) {
return []*model.Lane{{UserID: uuid.New()}}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/lanes", h.ListLanes)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/lanes", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("ListLanes success: want 200, got %d", w.Code)
}
}

func TestLaneHandler_CreateLane_Success(t *testing.T) {
h := &LaneHandler{Lanes: &mockLaneStore{}}
r := gin.New()
r.Use(injectTestUser())
r.POST("/lanes", h.CreateLane)
body := jsonBody(t, map[string]any{"title": "My Lane"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/lanes", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusCreated {
t.Errorf("CreateLane success: want 201, got %d", w.Code)
}
}

func TestLaneHandler_ReorderLanes_EmptyBody_Returns204(t *testing.T) {
h := &LaneHandler{Lanes: &mockLaneStore{}}
r := gin.New()
r.Use(injectTestUser())
r.POST("/lanes/reorder", h.ReorderLanes)
body := jsonBody(t, []any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/lanes/reorder", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNoContent {
t.Errorf("ReorderLanes empty: want 204, got %d", w.Code)
}
}

// ─── PageHandler – UpdatePage missing branches ────────────────────────────────

func TestPageHandler_UpdatePage_CycleDetection_Returns400(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Type: model.NodeTypePage, Tags: []string{}, ParentID: &pageID}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/pages/:id", h.UpdatePage)
body := jsonBody(t, map[string]any{"parentId": pageID.String()})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/pages/"+pageID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusBadRequest {
t.Errorf("UpdatePage self-parent cycle: want 400, got %d", w.Code)
}
}

func TestPageHandler_UpdatePage_IsAncestorError_Returns500(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
parentID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Type: model.NodeTypePage, Tags: []string{}, ParentID: &parentID}, nil
},
isAncestorFn: func(_, _ uuid.UUID) (bool, error) {
return false, errors.New("ancestor check error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/pages/:id", h.UpdatePage)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/pages/"+pageID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("UpdatePage ancestor error: want 500, got %d", w.Code)
}
}

func TestPageHandler_UpdatePage_IsAncestor_Returns400(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
parentID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Type: model.NodeTypePage, Tags: []string{}, ParentID: &parentID}, nil
},
isAncestorFn: func(_, _ uuid.UUID) (bool, error) {
return true, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/pages/:id", h.UpdatePage)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/pages/"+pageID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusBadRequest {
t.Errorf("UpdatePage ancestor cycle: want 400, got %d", w.Code)
}
}

func TestPageHandler_UpdatePage_UpdateStoreError_Returns500(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Type: model.NodeTypePage, Tags: []string{}}, nil
},
updateFn: func(p *model.Page) (*model.Page, error) {
return nil, errors.New("update error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/pages/:id", h.UpdatePage)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/pages/"+pageID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("UpdatePage update error: want 500, got %d", w.Code)
}
}

// ─── PageHandler – DeletePage additional paths ────────────────────────────────

func TestPageHandler_DeletePage_DeleteStoreError_Returns404(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Type: model.NodeTypePage, Tags: []string{}}, nil
},
deleteFn: func(id, userID uuid.UUID) error {
return store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/pages/:id", h.DeletePage)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/pages/"+pageID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("DeletePage delete error: want 404, got %d", w.Code)
}
}

func TestPageHandler_DeletePage_Success_Returns204(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Type: model.NodeTypePage, Tags: []string{}}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/pages/:id", h.DeletePage)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/pages/"+pageID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNoContent {
t.Errorf("DeletePage success: want 204, got %d", w.Code)
}
}

// ─── PageHandler – UpsertPageContent GetByID error ───────────────────────────

func TestPageHandler_UpsertPageContent_GetByIDError_Returns404(t *testing.T) {
h := &PageHandler{Pages: &mockPageStore{}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+uuid.New().String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("UpsertPageContent GetByID error: want 404, got %d", w.Code)
}
}

// ─── PageHandler – UpsertPage store error ────────────────────────────────────

func TestPageHandler_UpsertPage_StoreError_Returns404(t *testing.T) {
h := &PageHandler{Pages: &mockPageStore{
upsertFn: func(p *model.Page) (*model.Page, error) {
return nil, store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id", h.UpsertPage)
pageID := uuid.New()
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("UpsertPage store error: want 404, got %d", w.Code)
}
}

// ─── TaskHandler – UpdateTask store error ────────────────────────────────────

func TestTaskHandler_UpdateTask_StoreError_Returns500(t *testing.T) {
callerID := testUser().ID
taskID := uuid.New()
h := &TaskHandler{Tasks: &mockTaskStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
return &model.Task{ID: taskID, UserID: callerID, Title: "t", Status: model.StatusTodo, Priority: model.PriorityNone, Tags: []string{}}, nil
},
updateFn: func(t *model.Task) (*model.Task, error) {
return nil, errors.New("update error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/tasks/:id", h.UpdateTask)
body := jsonBody(t, map[string]any{"title": "updated"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("UpdateTask store error: want 500, got %d", w.Code)
}
}

// ─── TaskHandler – DeleteTask store error ────────────────────────────────────

func TestTaskHandler_DeleteTask_DeleteStoreError_Returns404(t *testing.T) {
callerID := testUser().ID
taskID := uuid.New()
h := &TaskHandler{Tasks: &mockTaskStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
return &model.Task{ID: taskID, UserID: callerID, Title: "t", Tags: []string{}}, nil
},
deleteFn: func(id, userID uuid.UUID) error {
return store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/tasks/:id", h.DeleteTask)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/tasks/"+taskID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("DeleteTask delete error: want 404, got %d", w.Code)
}
}

// ─── TemplateHandler – UpdateTemplate/DeleteTemplate/ListTemplates/CreateTemplate errors ─

func TestTemplateHandler_UpdateTemplate_UpdateError_Returns500(t *testing.T) {
callerID := testUser().ID
templateID := uuid.New()
h := &TemplateHandler{Templates: &mockTemplateStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Template, error) {
return &model.Template{ID: templateID, UserID: callerID}, nil
},
updateFn: func(_ *model.Template) (*model.Template, error) {
return nil, errors.New("update error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/templates/:id", h.UpdateTemplate)
body := jsonBody(t, map[string]any{"name": "updated"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/templates/"+templateID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("UpdateTemplate update error: want 500, got %d", w.Code)
}
}

func TestTemplateHandler_DeleteTemplate_DeleteError_Returns404(t *testing.T) {
callerID := testUser().ID
templateID := uuid.New()
h := &TemplateHandler{Templates: &mockTemplateStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Template, error) {
return &model.Template{ID: templateID, UserID: callerID}, nil
},
deleteFn: func(id, userID uuid.UUID) error {
return store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/templates/:id", h.DeleteTemplate)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/templates/"+templateID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("DeleteTemplate delete error: want 404, got %d", w.Code)
}
}

func TestTemplateHandler_ListTemplates_Error_Returns500(t *testing.T) {
h := &TemplateHandler{Templates: &mockTemplateStore{
listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Template, error) {
return nil, errors.New("list error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/templates", h.ListTemplates)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/templates", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("ListTemplates error: want 500, got %d", w.Code)
}
}

func TestTemplateHandler_CreateTemplate_Error_Returns500(t *testing.T) {
h := &TemplateHandler{Templates: &mockTemplateStore{
createFn: func(_ context.Context, t *model.Template) (*model.Template, error) {
return nil, errors.New("create error")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.POST("/templates", h.CreateTemplate)
body := jsonBody(t, map[string]any{"name": "t"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/templates", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("CreateTemplate error: want 500, got %d", w.Code)
}
}

// ─── ShareHandler – SearchUsers missing paths ─────────────────────────────────

func TestShareHandler_SearchUsers_SearchError_Returns500(t *testing.T) {
h := &ShareHandler{
Orgs:  &mockOrgStore{},
Users: &mockUserStore{
searchFn: func(query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error) {
return nil, errors.New("search error")
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.GET("/users/search", h.SearchUsers)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/users/search?q=test", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("SearchUsers search error: want 500, got %d", w.Code)
}
}

func TestShareHandler_SearchUsers_Success(t *testing.T) {
h := &ShareHandler{
Orgs:  &mockOrgStore{},
Users: &mockUserStore{
searchFn: func(query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error) {
email := "user@example.com"; return []*model.UserSearchResult{{ID: uuid.New(), Email: &email}}, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.GET("/users/search", h.SearchUsers)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/users/search?q=test", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("SearchUsers success: want 200, got %d", w.Code)
}
}

// ─── ShareHandler – UpdateSharePermission missing paths ──────────────────────

func TestShareHandler_UpdateSharePermission_Success(t *testing.T) {
shareID := uuid.New()
pageID := uuid.New()
callerID := testUser().ID
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(id uuid.UUID) (*model.Share, error) {
return &model.Share{
ID:           shareID,
SharedByID:   callerID,
ResourceType: model.ShareResourcePage,
ResourceID:   pageID,
Permission:   model.SharePermissionViewer,
}, nil
},
updatePermissionFn: func(id uuid.UUID, perm model.SharePermission) (*model.Share, error) {
return &model.Share{ID: id, Permission: perm}, nil
},
},
Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: callerID, Tags: []string{}}, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/shares/:shareId", h.UpdateSharePermission)
body := jsonBody(t, map[string]any{"permission": "editor"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/shares/"+shareID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpdateSharePermission success: want 200, got %d", w.Code)
}
}

// ─── isPrivateHost – link-local and unspecified IPs ──────────────────────────

func TestIsPrivateHost_LinkLocalIPs(t *testing.T) {
for _, ip := range []string{"169.254.1.1", "fe80::1"} {
if !isPrivateHost(ip) {
t.Errorf("isPrivateHost(%q) = false, want true", ip)
}
}
}

func TestIsPrivateHost_LoopbackIPv6(t *testing.T) {
if !isPrivateHost("::1") {
t.Error("isPrivateHost(::1) should return true")
}
}

// ─── UpsertPageContent additional paths ──────────────────────────────────────

func TestPageHandler_UpsertPageContent_OwnerEmptyContent_Returns200(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpsertPageContent empty content: want 200, got %d", w.Code)
}
}

func TestPageHandler_UpsertPageContent_InvalidContent_Returns400(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
// Content is non-empty but top-level type is not "doc".
body := jsonBody(t, map[string]any{"content": json.RawMessage(`{"type":"paragraph"}`)})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusBadRequest {
t.Errorf("UpsertPageContent invalid content: want 400, got %d", w.Code)
}
}

func TestPageHandler_UpsertPageContent_UpsertContentError_Returns500(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
upsertContentFn: func(_ *model.PageContent, _ uuid.UUID) (*model.PageContent, error) {
return nil, errors.New("db failure")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("UpsertPageContent upsert error: want 500, got %d", w.Code)
}
}

func TestPageHandler_UpsertPageContent_NonOwner_Denied_Returns403(t *testing.T) {
ownerID := uuid.New() // different from testUser()
pageID := uuid.New()
h := &PageHandler{
Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
},
Perms: &PermissionChecker{
Orgs:   &mockOrgStore{},   // GetMember returns ErrNotFound
Shares: &mockShareStore{}, // GetForUserAndResource returns ErrNotFound
},
}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusForbidden {
t.Errorf("UpsertPageContent non-owner denied: want 403, got %d", w.Code)
}
}

func TestPageHandler_UpsertPageContent_NonOwner_WithShare_Returns200(t *testing.T) {
ownerID := uuid.New() // different from testUser()
callerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{
Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
},
Perms: &PermissionChecker{
Orgs: &mockOrgStore{},
Shares: &mockShareStore{
getForUserAndResourceFn: func(_ context.Context, userID uuid.UUID, _ model.ShareResourceType, _ uuid.UUID) (*model.Share, error) {
if userID == callerID {
return &model.Share{Permission: model.SharePermissionEditor}, nil
}
return nil, store.ErrNotFound
},
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
body := jsonBody(t, map[string]any{})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpsertPageContent non-owner with share: want 200, got %d", w.Code)
}
}

// ─── UpdateTask – non-owner denied ───────────────────────────────────────────

func TestTaskHandler_UpdateTask_NonOwner_Denied_Returns403(t *testing.T) {
ownerID := uuid.New() // different from testUser()
taskID := uuid.New()
h := &TaskHandler{
Tasks: &mockTaskStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Task, error) {
return &model.Task{ID: taskID, UserID: ownerID, Title: "t", Status: model.StatusTodo, Priority: model.PriorityNone, Tags: []string{}}, nil
},
},
Perms: &PermissionChecker{
Orgs:   &mockOrgStore{},
Shares: &mockShareStore{},
},
}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/tasks/:id", h.UpdateTask)
body := jsonBody(t, map[string]any{"title": "updated"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusForbidden {
t.Errorf("UpdateTask non-owner denied: want 403, got %d", w.Code)
}
}

// ─── UpsertTask – store error ─────────────────────────────────────────────────

func TestTaskHandler_UpsertTask_StoreError_Returns404(t *testing.T) {
h := &TaskHandler{Tasks: &mockTaskStore{
upsertFn: func(_ *model.Task) (*model.Task, error) {
return nil, store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/tasks/:id", h.UpsertTask)
body := jsonBody(t, map[string]any{"title": "t"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/tasks/"+uuid.New().String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("UpsertTask store error: want 404, got %d", w.Code)
}
}

// ─── ListTasks – sourceNodeId success ────────────────────────────────────────

func TestTaskHandler_ListTasks_SourceNodeId_Success(t *testing.T) {
h := &TaskHandler{Tasks: &mockTaskStore{
listBySourceNodeFn: func(_ context.Context, _ uuid.UUID, _ string) ([]*model.Task, error) {
return []*model.Task{}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/tasks", h.ListTasks)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/tasks?sourceNodeId=some-node-id", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("ListTasks sourceNodeId success: want 200, got %d", w.Code)
}
}

func TestTaskHandler_ListTasks_SourcePageId_Success(t *testing.T) {
h := &TaskHandler{Tasks: &mockTaskStore{
listBySourcePageFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]*model.Task, error) {
return []*model.Task{}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/tasks", h.ListTasks)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/tasks?sourcePageId="+uuid.New().String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("ListTasks sourcePageId success: want 200, got %d", w.Code)
}
}

func TestTaskHandler_ListTasks_PaginatedError_Returns500(t *testing.T) {
h := &TaskHandler{Tasks: &mockTaskStore{
listByUserPaginatedFn: func(_ context.Context, _ uuid.UUID, _ store.Pagination) ([]*model.Task, int, error) {
return nil, 0, errors.New("db failure")
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/tasks", h.ListTasks)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/tasks?limit=10", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("ListTasks paginated error: want 500, got %d", w.Code)
}
}

// ─── UpsertTemplate – store error ────────────────────────────────────────────

func TestTemplateHandler_UpsertTemplate_StoreError_Returns404(t *testing.T) {
h := &TemplateHandler{Templates: &mockTemplateStore{
upsertFn: func(_ *model.Template) (*model.Template, error) {
return nil, store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/templates/:id", h.UpsertTemplate)
body := jsonBody(t, map[string]any{"name": "t"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/templates/"+uuid.New().String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("UpsertTemplate store error: want 404, got %d", w.Code)
}
}

// ─── ReorderLanes – success with items ───────────────────────────────────────

func TestLaneHandler_ReorderLanes_WithItems_Returns204(t *testing.T) {
h := &LaneHandler{Lanes: &mockLaneStore{}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/lanes/order", h.ReorderLanes)
body := jsonBody(t, []store.LaneReorderItem{{ID: uuid.New(), Order: 0}})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/lanes/order", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusNoContent {
t.Errorf("ReorderLanes with items: want 204, got %d", w.Code)
}
}

// ─── CreateOrg – success ─────────────────────────────────────────────────────

func TestOrgHandler_CreateOrg_Success_Returns201(t *testing.T) {
created := &model.Organization{ID: uuid.New(), Name: "My Org"}
h := &OrgHandler{
Orgs: &mockOrgStore{
createOrgFn: func(_ *model.Organization) (*model.Organization, error) {
return created, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.POST("/orgs", h.CreateOrg)
body := jsonBody(t, map[string]string{"name": "My Org"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/orgs", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusCreated {
t.Errorf("CreateOrg success: want 201, got %d", w.Code)
}
}

// ─── GetOrg – GetMember error ────────────────────────────────────────────────

func TestOrgHandler_GetOrg_GetMemberError_Returns404(t *testing.T) {
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
return nil, store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/orgs/:orgId", h.GetOrg)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/orgs/"+uuid.New().String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("GetOrg GetMember error: want 404, got %d", w.Code)
}
}

// ─── UpdateOrg – success ─────────────────────────────────────────────────────

func TestOrgHandler_UpdateOrg_Success_Returns200(t *testing.T) {
orgID := uuid.New()
h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
getByIDFn: func(id uuid.UUID) (*model.Organization, error) {
return &model.Organization{ID: id, Name: "Old Name"}, nil
},
updateOrgFn: func(org *model.Organization) (*model.Organization, error) {
return org, nil
},
})}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId", h.UpdateOrg)
body := jsonBody(t, map[string]string{"name": "New Name"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/orgs/"+orgID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpdateOrg success: want 200, got %d", w.Code)
}
}

// ─── AddOrgMember – success paths ────────────────────────────────────────────

func TestOrgHandler_AddOrgMember_ByUserID_Success_Returns201(t *testing.T) {
memberID := uuid.New()
h := &OrgHandler{
Orgs: ownerOrgStore(&mockOrgStore{
addMemberFn: func(_, _ uuid.UUID, _ model.OrgRole) (*model.OrgMember, error) {
return &model.OrgMember{UserID: memberID, Role: model.OrgRoleViewer}, nil
},
}),
Users: &mockUserStore{
getByIDFn: func(_ uuid.UUID) (*model.User, error) {
return &model.User{ID: memberID}, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.POST("/orgs/:orgId/members", h.AddOrgMember)
body := jsonBody(t, map[string]string{"userId": memberID.String(), "role": "viewer"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusCreated {
t.Errorf("AddOrgMember byUserID success: want 201, got %d", w.Code)
}
}

func TestOrgHandler_AddOrgMember_ByEmail_Success_Returns201(t *testing.T) {
memberID := uuid.New()
h := &OrgHandler{
Orgs: ownerOrgStore(&mockOrgStore{
addMemberFn: func(_, _ uuid.UUID, _ model.OrgRole) (*model.OrgMember, error) {
return &model.OrgMember{UserID: memberID, Role: model.OrgRoleViewer}, nil
},
}),
Users: &mockUserStore{
getByEmailFn: func(_ string) (*model.User, error) {
return &model.User{ID: memberID}, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.POST("/orgs/:orgId/members", h.AddOrgMember)
body := jsonBody(t, map[string]string{"email": "member@example.com"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusCreated {
t.Errorf("AddOrgMember byEmail success: want 201, got %d", w.Code)
}
}

func TestOrgHandler_AddOrgMember_AddMemberError_Returns500(t *testing.T) {
memberID := uuid.New()
h := &OrgHandler{
Orgs: ownerOrgStore(&mockOrgStore{
addMemberFn: func(_, _ uuid.UUID, _ model.OrgRole) (*model.OrgMember, error) {
return nil, errors.New("db failure")
},
}),
Users: &mockUserStore{
getByIDFn: func(_ uuid.UUID) (*model.User, error) {
return &model.User{ID: memberID}, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.POST("/orgs/:orgId/members", h.AddOrgMember)
body := jsonBody(t, map[string]string{"userId": memberID.String()})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("AddOrgMember store error: want 500, got %d", w.Code)
}
}

// ─── UpdateOrgMemberRole – success paths ─────────────────────────────────────

func TestOrgHandler_UpdateOrgMemberRole_RoleOwner_SkipsDemoteCheck_Returns200(t *testing.T) {
// When new role is Owner, no GetMember/countOwners check is performed.
memberID := uuid.New()
h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
updateMemberRoleFn: func(_, _ uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
return &model.OrgMember{UserID: memberID, Role: role}, nil
},
})}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)
body := jsonBody(t, map[string]string{"role": "owner"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpdateOrgMemberRole owner role: want 200, got %d", w.Code)
}
}

func TestOrgHandler_UpdateOrgMemberRole_ExistingViewer_Returns200(t *testing.T) {
// When existing role is Viewer (not Owner), demote check is skipped.
memberID := uuid.New()
callerID := testUser().ID
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
if userID == callerID {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
}
return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
},
updateMemberRoleFn: func(_, _ uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
return &model.OrgMember{UserID: memberID, Role: role}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)
body := jsonBody(t, map[string]string{"role": "editor"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpdateOrgMemberRole existing viewer: want 200, got %d", w.Code)
}
}

// ─── RemoveOrgMember – self-leave ────────────────────────────────────────────

func TestOrgHandler_RemoveOrgMember_SelfLeave_Returns204(t *testing.T) {
// When memberID == user.ID, orgOwnerGuard is bypassed.
callerID := testUser().ID
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
return &model.OrgMember{UserID: callerID, Role: model.OrgRoleViewer}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/"+callerID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNoContent {
t.Errorf("RemoveOrgMember self-leave: want 204, got %d", w.Code)
}
}

// ─── isResourceOwner – Template GetByID error ────────────────────────────────

func TestShareHandler_isResourceOwner_TemplateGetByIDError_Returns404(t *testing.T) {
tmplID := uuid.New()
shareID := uuid.New()
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
return &model.Share{
ResourceType: model.ShareResourceTemplate,
ResourceID:   tmplID,
}, nil
},
},
Templates: &mockTemplateStore{
getByIDFn: func(_, _ uuid.UUID) (*model.Template, error) {
return nil, store.ErrNotFound
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/shares/:shareId", h.DeleteShare)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/shares/"+shareID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("isResourceOwner template GetByID error: want 404, got %d", w.Code)
}
}

// ─── UpdateSharePermission – isResourceOwner denied ──────────────────────────

func TestShareHandler_UpdateSharePermission_NonOwner_Returns403(t *testing.T) {
ownerID := uuid.New() // different from testUser()
pageID := uuid.New()
shareID := uuid.New()
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
return &model.Share{
ResourceType: model.ShareResourcePage,
ResourceID:   pageID,
}, nil
},
},
Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
},
}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/shares/:shareId", h.UpdateSharePermission)
body := jsonBody(t, map[string]any{"permission": "editor"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/shares/"+shareID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusForbidden {
t.Errorf("UpdateSharePermission non-owner: want 403, got %d", w.Code)
}
}

// ─── FilterTasks – bad JSON body ─────────────────────────────────────────────

func TestTaskHandler_FilterTasks_BadJSON_Returns400(t *testing.T) {
h := &TaskHandler{Tasks: &mockTaskStore{}}
r := gin.New()
r.Use(injectTestUser())
r.POST("/tasks/filter", h.FilterTasks)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/tasks/filter", nil)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusBadRequest {
t.Errorf("FilterTasks bad JSON: want 400, got %d", w.Code)
}
}

// ─── ListTemplates – success ──────────────────────────────────────────────────

func TestTemplateHandler_ListTemplates_Success_Returns200(t *testing.T) {
h := &TemplateHandler{Templates: &mockTemplateStore{
listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Template, error) {
return []*model.Template{}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/templates", h.ListTemplates)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/templates", nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("ListTemplates success: want 200, got %d", w.Code)
}
}

// ─── CreateTemplate – success ────────────────────────────────────────────────

func TestTemplateHandler_CreateTemplate_Success_Returns201(t *testing.T) {
h := &TemplateHandler{Templates: &mockTemplateStore{}}
r := gin.New()
r.Use(injectTestUser())
r.POST("/templates", h.CreateTemplate)
body := jsonBody(t, map[string]any{"name": "My Template"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/templates", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusCreated {
t.Errorf("CreateTemplate success: want 201, got %d", w.Code)
}
}

// ─── UpsertPageContent – valid doc content ───────────────────────────────────

func TestPageHandler_UpsertPageContent_ValidDocContent_Returns200(t *testing.T) {
ownerID := testUser().ID
pageID := uuid.New()
h := &PageHandler{Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return &model.Page{ID: pageID, UserID: ownerID, Tags: []string{}}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PUT("/pages/:id/content", h.UpsertPageContent)
body := jsonBody(t, map[string]any{
"content": json.RawMessage(`{"type":"doc","content":[]}`),
})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpsertPageContent valid doc content: want 200, got %d", w.Code)
}
}

// ─── DeletePage – GetByID error ───────────────────────────────────────────────

func TestPageHandler_DeletePage_GetByIDError_Returns404(t *testing.T) {
h := &PageHandler{Pages: &mockPageStore{}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/pages/:id", h.DeletePage)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/pages/"+uuid.New().String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("DeletePage GetByID error: want 404, got %d", w.Code)
}
}

// ─── RemoveOrgMember – owner with multiple owners ────────────────────────────

func TestOrgHandler_RemoveOrgMember_OwnerWithMultipleOwners_Returns204(t *testing.T) {
memberID := uuid.New()
callerID := testUser().ID
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
if userID == callerID {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
}
// member being removed is also an owner
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
},
listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
// Multiple owners — removal allowed
return []*model.OrgMember{
{Role: model.OrgRoleOwner},
{Role: model.OrgRoleOwner},
}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNoContent {
t.Errorf("RemoveOrgMember owner with multiple owners: want 204, got %d", w.Code)
}
}

// ─── UpdateOrgMemberRole – demoting owner allowed with multiple owners ────────

func TestOrgHandler_UpdateOrgMemberRole_DemoteOwnerMultiple_Returns200(t *testing.T) {
memberID := uuid.New()
callerID := testUser().ID
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
if userID == callerID {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
}
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
},
listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
return []*model.OrgMember{
{Role: model.OrgRoleOwner},
{Role: model.OrgRoleOwner},
}, nil
},
updateMemberRoleFn: func(_, _ uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
return &model.OrgMember{UserID: memberID, Role: role}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)
body := jsonBody(t, map[string]string{"role": "editor"})
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), body)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)
if w.Code != http.StatusOK {
t.Errorf("UpdateOrgMemberRole demote owner with multiple owners: want 200, got %d", w.Code)
}
}

// ─── RemoveOrgMember – second GetMember error ─────────────────────────────────

func TestOrgHandler_RemoveOrgMember_GetMemberBodyError_Returns404(t *testing.T) {
// caller passes orgOwnerGuard but the target member is not found
callerID := testUser().ID
memberID := uuid.New()
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
if userID == callerID {
// orgOwnerGuard succeeds for caller
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
}
// second GetMember (for memberID) returns not found
return nil, store.ErrNotFound
},
}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusNotFound {
t.Errorf("RemoveOrgMember second GetMember error: want 404, got %d", w.Code)
}
}

func TestOrgHandler_RemoveOrgMember_CountOwnersError_Returns500(t *testing.T) {
// both caller and member are owners, but listMembers fails
callerID := testUser().ID
memberID := uuid.New()
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
},
listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
return nil, errors.New("db failure")
},
}}
_ = callerID
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), nil)
r.ServeHTTP(w, req)
if w.Code != http.StatusInternalServerError {
t.Errorf("RemoveOrgMember countOrgOwners error: want 500, got %d", w.Code)
}
}
