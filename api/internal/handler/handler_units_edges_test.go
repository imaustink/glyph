package handler

import (
	"bytes"
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

func badJSONBody() *bytes.Reader {
	return bytes.NewReader([]byte(`{bad json}`))
}

func jsonBodyBytes(v any) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// ─── LaneHandler edge cases ──────────────────────────────────────────────────

func TestLaneHandler_CreateLane_BadJSON_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes", h.CreateLane)

	req := httptest.NewRequest(http.MethodPost, "/lanes", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateLane bad json: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_BatchCreateLanes_BadJSON_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes/batch", h.BatchCreateLanes)

	req := httptest.NewRequest(http.MethodPost, "/lanes/batch", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("BatchCreateLanes bad json: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_UpdateLane_InvalidID_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/lanes/:id", h.UpdateLane)

	req := httptest.NewRequest(http.MethodPatch, "/lanes/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateLane invalid id: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_UpdateLane_GetByIDError_Returns404(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{
		getByIDFn: func(id, userID uuid.UUID) (*model.Lane, error) { return nil, store.ErrNotFound },
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/lanes/:id", h.UpdateLane)

	req := httptest.NewRequest(http.MethodPatch, "/lanes/"+uuid.New().String(), jsonBodyBytes(map[string]string{"title": "x"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("UpdateLane get by id error: want 404, got %d", w.Code)
	}
}

func TestLaneHandler_UpdateLane_UpdateError_Returns500_Edge(t *testing.T) {
	lid := uuid.New()
	uid := testUser().ID
	h := &LaneHandler{Lanes: &mockLaneStore{
		getByIDFn: func(id, userID uuid.UUID) (*model.Lane, error) {
			return &model.Lane{ID: lid, UserID: uid}, nil
		},
		updateFn: func(l *model.Lane) (*model.Lane, error) { return nil, errors.New("db") },
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/lanes/:id", h.UpdateLane)

	req := httptest.NewRequest(http.MethodPatch, "/lanes/"+lid.String(), jsonBodyBytes(map[string]string{"title": "x"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdateLane update error: want 500, got %d", w.Code)
	}
}

func TestLaneHandler_DeleteLane_InvalidID_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/lanes/:id", h.DeleteLane)

	req := httptest.NewRequest(http.MethodDelete, "/lanes/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DeleteLane invalid id: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_UpsertLane_InvalidID_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/lanes/:id", h.UpsertLane)

	req := httptest.NewRequest(http.MethodPut, "/lanes/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertLane invalid id: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_UpsertLane_BadJSON_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/lanes/:id", h.UpsertLane)

	req := httptest.NewRequest(http.MethodPut, "/lanes/"+uuid.New().String(), badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertLane bad json: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_ReorderLanes_BadJSON_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes/reorder", h.ReorderLanes)

	req := httptest.NewRequest(http.MethodPost, "/lanes/reorder", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("ReorderLanes bad json: want 400, got %d", w.Code)
	}
}

// ─── OrgHandler edge cases ───────────────────────────────────────────────────

func TestOrgHandler_CreateOrg_BadJSON_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs", h.CreateOrg)

	req := httptest.NewRequest(http.MethodPost, "/orgs", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateOrg bad json: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_GetOrg_InvalidID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/orgs/:orgId", h.GetOrg)

	req := httptest.NewRequest(http.MethodGet, "/orgs/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GetOrg invalid id: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrg_InvalidID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId", h.UpdateOrg)

	req := httptest.NewRequest(http.MethodPatch, "/orgs/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrg invalid id: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrg_BadJSON_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore()}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId", h.UpdateOrg)

	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String(), badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrg bad json: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_DeleteOrg_InvalidID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId", h.DeleteOrg)

	req := httptest.NewRequest(http.MethodDelete, "/orgs/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DeleteOrg invalid id: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_InvalidOrgID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	req := httptest.NewRequest(http.MethodPost, "/orgs/not-a-uuid/members", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("AddOrgMember invalid orgId: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_BadJSON_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore()}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("AddOrgMember bad json: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_MissingUserIDAndEmail_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore()}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members",
		jsonBodyBytes(map[string]string{"role": "viewer"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("AddOrgMember missing userId/email: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrgMemberRole_InvalidOrgID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	req := httptest.NewRequest(http.MethodPatch, "/orgs/not-a-uuid/members/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrgMemberRole invalid orgId: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrgMemberRole_InvalidUserID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore()}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrgMemberRole invalid userId: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrgMemberRole_BadJSON_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore()}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+uuid.New().String(), badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrgMemberRole bad json: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_RemoveOrgMember_InvalidOrgID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)

	req := httptest.NewRequest(http.MethodDelete, "/orgs/not-a-uuid/members/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("RemoveOrgMember invalid orgId: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_RemoveOrgMember_InvalidUserID_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore()}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)

	req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("RemoveOrgMember invalid userId: want 400, got %d", w.Code)
	}
}

// ─── PageHandler edge cases ──────────────────────────────────────────────────

func TestPageHandler_ListPages_BadOffset_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages", h.ListPages)

	req := httptest.NewRequest(http.MethodGet, "/pages?offset=bad", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("ListPages bad offset: want 400, got %d", w.Code)
	}
}

func TestPageHandler_CreatePage_BadJSON_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/pages", h.CreatePage)

	req := httptest.NewRequest(http.MethodPost, "/pages", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreatePage bad json: want 400, got %d", w.Code)
	}
}

func TestPageHandler_GetPage_InvalidID_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages/:id", h.GetPage)

	req := httptest.NewRequest(http.MethodGet, "/pages/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GetPage invalid id: want 400, got %d", w.Code)
	}
}

func TestPageHandler_UpdatePage_InvalidID_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/pages/:id", h.UpdatePage)

	req := httptest.NewRequest(http.MethodPatch, "/pages/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdatePage invalid id: want 400, got %d", w.Code)
	}
}

func TestPageHandler_UpdatePage_CircularRef_Returns400(t *testing.T) {
	uid := testUser().ID
	pid := uuid.New()
	parentID := uuid.New()
	h := &PageHandler{Pages: &mockPageStore{
		getByIDFn: func(id, userID uuid.UUID) (*model.Page, error) {
			return &model.Page{ID: id, UserID: uid, ParentID: &parentID}, nil
		},
		isAncestorFn: func(candidate, node uuid.UUID) (bool, error) {
			return true, nil
		},
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/pages/:id", h.UpdatePage)

	req := httptest.NewRequest(http.MethodPatch, "/pages/"+pid.String(),
		jsonBodyBytes(map[string]interface{}{"title": "x"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdatePage circular ref: want 400, got %d", w.Code)
	}
}

func TestPageHandler_UpdatePage_UpdateError_Returns500(t *testing.T) {
	uid := testUser().ID
	pid := uuid.New()
	h := &PageHandler{Pages: &mockPageStore{
		getByIDFn: func(id, userID uuid.UUID) (*model.Page, error) {
			return &model.Page{ID: id, UserID: uid}, nil
		},
		updateFn: func(p *model.Page) (*model.Page, error) { return nil, errors.New("db") },
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/pages/:id", h.UpdatePage)

	req := httptest.NewRequest(http.MethodPatch, "/pages/"+pid.String(),
		jsonBodyBytes(map[string]interface{}{"title": "x"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdatePage update error: want 500, got %d", w.Code)
	}
}

func TestPageHandler_UpsertPage_InvalidID_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/pages/:id", h.UpsertPage)

	req := httptest.NewRequest(http.MethodPut, "/pages/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertPage invalid id: want 400, got %d", w.Code)
	}
}

func TestPageHandler_UpsertPage_BadJSON_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/pages/:id", h.UpsertPage)

	req := httptest.NewRequest(http.MethodPut, "/pages/"+uuid.New().String(), badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertPage bad json: want 400, got %d", w.Code)
	}
}

func TestPageHandler_GetPageContent_InvalidID_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages/:id/content", h.GetPageContent)

	req := httptest.NewRequest(http.MethodGet, "/pages/not-a-uuid/content", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GetPageContent invalid id: want 400, got %d", w.Code)
	}
}

func TestPageHandler_UpsertPageContent_InvalidID_Returns400(t *testing.T) {
	h := &PageHandler{Pages: &mockPageStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/pages/:id/content", h.UpsertPageContent)

	req := httptest.NewRequest(http.MethodPut, "/pages/not-a-uuid/content", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertPageContent invalid id: want 400, got %d", w.Code)
	}
}

// ─── TaskHandler edge cases ──────────────────────────────────────────────────

func TestTaskHandler_CreateTask_BadJSON_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/tasks", h.CreateTask)

	req := httptest.NewRequest(http.MethodPost, "/tasks", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateTask bad json: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_GetTask_InvalidID_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks/:id", h.GetTask)

	req := httptest.NewRequest(http.MethodGet, "/tasks/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GetTask invalid id: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_UpdateTask_InvalidID_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/tasks/:id", h.UpdateTask)

	req := httptest.NewRequest(http.MethodPatch, "/tasks/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateTask invalid id: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_DeleteTask_InvalidID_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/tasks/:id", h.DeleteTask)

	req := httptest.NewRequest(http.MethodDelete, "/tasks/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DeleteTask invalid id: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_UpsertTask_InvalidID_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/tasks/:id", h.UpsertTask)

	req := httptest.NewRequest(http.MethodPut, "/tasks/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertTask invalid id: want 400, got %d", w.Code)
	}
}

func TestTaskHandler_UpsertTask_BadJSON_Returns400(t *testing.T) {
	h := &TaskHandler{Tasks: &mockTaskStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/tasks/:id", h.UpsertTask)

	req := httptest.NewRequest(http.MethodPut, "/tasks/"+uuid.New().String(), badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertTask bad json: want 400, got %d", w.Code)
	}
}

// ─── TemplateHandler edge cases ──────────────────────────────────────────────

func TestTemplateHandler_CreateTemplate_BadJSON_Returns400(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/templates", h.CreateTemplate)

	req := httptest.NewRequest(http.MethodPost, "/templates", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateTemplate bad json: want 400, got %d", w.Code)
	}
}

func TestTemplateHandler_GetTemplate_InvalidID_Returns400(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/templates/:id", h.GetTemplate)

	req := httptest.NewRequest(http.MethodGet, "/templates/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GetTemplate invalid id: want 400, got %d", w.Code)
	}
}

func TestTemplateHandler_UpdateTemplate_InvalidID_Returns400(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/templates/:id", h.UpdateTemplate)

	req := httptest.NewRequest(http.MethodPatch, "/templates/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateTemplate invalid id: want 400, got %d", w.Code)
	}
}

func TestTemplateHandler_UpdateTemplate_UpdateError_Returns500_Edge(t *testing.T) {
	uid := testUser().ID
	h := &TemplateHandler{Templates: &mockTemplateStore{
		getByIDFn: func(id, userID uuid.UUID) (*model.Template, error) {
			return &model.Template{ID: id, UserID: uid, Name: "t"}, nil
		},
		updateFn: func(t *model.Template) (*model.Template, error) { return nil, errors.New("db") },
	}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/templates/:id", h.UpdateTemplate)

	req := httptest.NewRequest(http.MethodPatch, "/templates/"+uuid.New().String(),
		jsonBodyBytes(map[string]string{"name": "new"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdateTemplate update error: want 500, got %d", w.Code)
	}
}

func TestTemplateHandler_DeleteTemplate_InvalidID_Returns400(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/templates/:id", h.DeleteTemplate)

	req := httptest.NewRequest(http.MethodDelete, "/templates/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DeleteTemplate invalid id: want 400, got %d", w.Code)
	}
}

func TestTemplateHandler_UpsertTemplate_InvalidID_Returns400(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/templates/:id", h.UpsertTemplate)

	req := httptest.NewRequest(http.MethodPut, "/templates/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertTemplate invalid id: want 400, got %d", w.Code)
	}
}

func TestTemplateHandler_UpsertTemplate_BadJSON_Returns400(t *testing.T) {
	h := &TemplateHandler{Templates: &mockTemplateStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/templates/:id", h.UpsertTemplate)

	req := httptest.NewRequest(http.MethodPut, "/templates/"+uuid.New().String(), badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpsertTemplate bad json: want 400, got %d", w.Code)
	}
}

// ─── ShareHandler edge cases ─────────────────────────────────────────────────

func TestShareHandler_CreateShare_BadJSON_Returns400(t *testing.T) {
	h := &ShareHandler{
		Shares: &mockShareStore{},
		Pages:  &mockPageStore{},
		Tasks:  &mockTaskStore{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/shares", h.CreateShare)

	req := httptest.NewRequest(http.MethodPost, "/shares", badJSONBody())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateShare bad json: want 400, got %d", w.Code)
	}
}

func TestShareHandler_UpdateSharePermission_InvalidID_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/shares/:shareId", h.UpdateSharePermission)

	req := httptest.NewRequest(http.MethodPatch, "/shares/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateSharePermission invalid id: want 400, got %d", w.Code)
	}
}

func TestShareHandler_UpdateSharePermission_UpdateError_Returns500(t *testing.T) {
	uid := testUser().ID
	shareID := uuid.New()
	resourceID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{
			getByIDShareFn: func(id uuid.UUID) (*model.Share, error) {
				return &model.Share{
					ID:           id,
					ResourceType: model.ShareResourcePage,
					ResourceID:   resourceID,
					SharedByID:   uid,
					Permission:   model.SharePermissionViewer,
				}, nil
			},
			updatePermissionFn: func(id uuid.UUID, perm model.SharePermission) (*model.Share, error) {
				return nil, errors.New("db error")
			},
		},
		Pages: &mockPageStore{
			getByIDFn: func(id, userID uuid.UUID) (*model.Page, error) {
				return &model.Page{ID: id, UserID: uid}, nil
			},
		},
		Tasks:     &mockTaskStore{},
		Templates: &mockTemplateStore{},
	}
	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/shares/:shareId", h.UpdateSharePermission)

	req := httptest.NewRequest(http.MethodPatch, "/shares/"+shareID.String(),
		jsonBodyBytes(map[string]string{"permission": "editor"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdateSharePermission update error: want 500, got %d", w.Code)
	}
}

func TestShareHandler_DeleteShare_InvalidID_Returns400(t *testing.T) {
	h := &ShareHandler{Shares: &mockShareStore{}}
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/shares/:shareId", h.DeleteShare)

	req := httptest.NewRequest(http.MethodDelete, "/shares/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DeleteShare invalid id: want 400, got %d", w.Code)
	}
}

// ─── content_validator edge cases ─────────────────────────────────────────────

func TestSanitizeNode_NonMapMark_IsSkipped(t *testing.T) {
	content := map[string]interface{}{
		"type": "doc",
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"marks": []interface{}{
					"this-is-a-string-not-a-map",
					map[string]interface{}{"type": "bold"},
				},
			},
		},
	}
	b, _ := json.Marshal(content)
	result, err := ValidateProseMirrorContent(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected sanitized content")
	}
}

func TestSanitizeNode_NonMapChild_IsSkipped(t *testing.T) {
	content := map[string]interface{}{
		"type": "doc",
		"content": []interface{}{
			"this-is-a-string-not-a-map",
			map[string]interface{}{"type": "paragraph"},
		},
	}
	b, _ := json.Marshal(content)
	result, err := ValidateProseMirrorContent(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected sanitized content")
	}
}

// ensure context import is used (context needed for mockTaskStore.listByUserFn)
var _ = context.Background
