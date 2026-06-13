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
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

func init() {
	RegisterValidators()
}

// ─── Mock stores ─────────────────────────────────────────────────────────────

type mockLaneStore struct {
	listByUserFn  func(ctx context.Context, userID uuid.UUID) ([]*model.Lane, error)
	createFn      func(ctx context.Context, l *model.Lane) (*model.Lane, error)
	batchCreateFn func(ctx context.Context, lanes []*model.Lane) ([]*model.Lane, error)
	reorderFn     func(ctx context.Context, userID uuid.UUID, items []store.LaneReorderItem) error
	getByIDFn     func(id, userID uuid.UUID) (*model.Lane, error)
	upsertFn      func(l *model.Lane) (*model.Lane, error)
	updateFn      func(l *model.Lane) (*model.Lane, error)
	deleteFn      func(id, userID uuid.UUID) error
}

func (m *mockLaneStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Lane, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockLaneStore) Create(ctx context.Context, l *model.Lane) (*model.Lane, error) {
	if m.createFn != nil {
		return m.createFn(ctx, l)
	}
	return l, nil
}
func (m *mockLaneStore) BatchCreate(ctx context.Context, lanes []*model.Lane) ([]*model.Lane, error) {
	if m.batchCreateFn != nil {
		return m.batchCreateFn(ctx, lanes)
	}
	return lanes, nil
}
func (m *mockLaneStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Lane, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id, userID)
	}
	return nil, store.ErrNotFound
}
func (m *mockLaneStore) Update(_ context.Context, l *model.Lane) (*model.Lane, error) {
	if m.updateFn != nil {
		return m.updateFn(l)
	}
	return l, nil
}
func (m *mockLaneStore) Upsert(_ context.Context, l *model.Lane) (*model.Lane, error) {
	if m.upsertFn != nil {
		return m.upsertFn(l)
	}
	return l, nil
}
func (m *mockLaneStore) ReorderAll(ctx context.Context, userID uuid.UUID, items []store.LaneReorderItem) error {
	if m.reorderFn != nil {
		return m.reorderFn(ctx, userID, items)
	}
	return nil
}
func (m *mockLaneStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id, userID)
	}
	return nil
}
func (m *mockLaneStore) ListByFolder(_ context.Context, folderID, userID uuid.UUID) ([]*model.Lane, error) {
	return nil, nil
}
func (m *mockLaneStore) GetByIDAndFolder(_ context.Context, id, folderID uuid.UUID) (*model.Lane, error) {
	return nil, store.ErrNotFound
}
func (m *mockLaneStore) UpdateByIDAndFolder(_ context.Context, l *model.Lane, folderID uuid.UUID) (*model.Lane, error) {
	return l, nil
}
func (m *mockLaneStore) DeleteByIDAndFolder(_ context.Context, id, folderID uuid.UUID) error {
	return nil
}

type mockTemplateStore struct {
	listByUserFn func(ctx context.Context, userID uuid.UUID) ([]*model.Template, error)
	createFn     func(ctx context.Context, t *model.Template) (*model.Template, error)
	getByIDFn    func(id, userID uuid.UUID) (*model.Template, error)
	updateFn     func(t *model.Template) (*model.Template, error)
	upsertFn     func(t *model.Template) (*model.Template, error)
	deleteFn     func(id, userID uuid.UUID) error
}

func (m *mockTemplateStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Template, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockTemplateStore) Create(ctx context.Context, t *model.Template) (*model.Template, error) {
	if m.createFn != nil {
		return m.createFn(ctx, t)
	}
	return t, nil
}
func (m *mockTemplateStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Template, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id, userID)
	}
	return nil, store.ErrNotFound
}
func (m *mockTemplateStore) Update(_ context.Context, t *model.Template) (*model.Template, error) {
	if m.updateFn != nil {
		return m.updateFn(t)
	}
	return t, nil
}
func (m *mockTemplateStore) Upsert(_ context.Context, t *model.Template) (*model.Template, error) {
	if m.upsertFn != nil {
		return m.upsertFn(t)
	}
	return t, nil
}
func (m *mockTemplateStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id, userID)
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// testUser returns a deterministic user for handler tests.
func testUser() *model.User {
	email := "test@example.com"
	return &model.User{ID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"), Email: &email}
}

// injectTestUser returns middleware that sets the test user in context.
func injectTestUser() gin.HandlerFunc {
	u := testUser()
	return func(c *gin.Context) {
		c.Set(auth.ContextKey, u)
		c.Next()
	}
}

// jsonBody serialises v and returns a reader suitable for http.Request.Body.
func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return bytes.NewReader(b)
}

// ─── LaneHandler tests ───────────────────────────────────────────────────────

func TestLaneHandler_ListLanes_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &LaneHandler{Lanes: &mockLaneStore{
		listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Lane, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/lanes", h.ListLanes)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/lanes", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListLanes store error: want 500, got %d", w.Code)
	}
}

func TestLaneHandler_CreateLane_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &LaneHandler{Lanes: &mockLaneStore{
		createFn: func(_ context.Context, _ *model.Lane) (*model.Lane, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes", h.CreateLane)

	body := jsonBody(t, model.Lane{Title: "My Lane"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/lanes", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreateLane store error: want 500, got %d", w.Code)
	}
}

func TestLaneHandler_BatchCreateLanes_EmptySlice_Returns200(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes/batch", h.BatchCreateLanes)

	body := jsonBody(t, []model.Lane{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/lanes/batch", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("BatchCreateLanes empty: want 200, got %d", w.Code)
	}
}

func TestLaneHandler_BatchCreateLanes_TooMany_Returns400(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes/batch", h.BatchCreateLanes)

	lanes := make([]model.Lane, 21)
	for i := range lanes {
		lanes[i] = model.Lane{Title: "lane"}
	}
	body := jsonBody(t, lanes)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/lanes/batch", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("BatchCreateLanes too many: want 400, got %d", w.Code)
	}
}

func TestLaneHandler_BatchCreateLanes_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &LaneHandler{Lanes: &mockLaneStore{
		batchCreateFn: func(_ context.Context, _ []*model.Lane) ([]*model.Lane, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/lanes/batch", h.BatchCreateLanes)

	body := jsonBody(t, []model.Lane{{Title: "lane"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/lanes/batch", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("BatchCreateLanes store error: want 500, got %d", w.Code)
	}
}

func TestLaneHandler_ReorderLanes_EmptyItems_Returns204(t *testing.T) {
	h := &LaneHandler{Lanes: &mockLaneStore{}}

	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/lanes/reorder", h.ReorderLanes)

	body := jsonBody(t, []store.LaneReorderItem{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/lanes/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("ReorderLanes empty: want 204, got %d", w.Code)
	}
}

func TestLaneHandler_ReorderLanes_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &LaneHandler{Lanes: &mockLaneStore{
		reorderFn: func(_ context.Context, _ uuid.UUID, _ []store.LaneReorderItem) error {
			return storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/lanes/reorder", h.ReorderLanes)

	items := []store.LaneReorderItem{{ID: uuid.New(), Order: 1}}
	body := jsonBody(t, items)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/lanes/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ReorderLanes store error: want 500, got %d", w.Code)
	}
}

// ─── TemplateHandler tests ───────────────────────────────────────────────────

func TestTemplateHandler_ListTemplates_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TemplateHandler{Templates: &mockTemplateStore{
		listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Template, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/templates", h.ListTemplates)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/templates", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListTemplates store error: want 500, got %d", w.Code)
	}
}

func TestTemplateHandler_CreateTemplate_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TemplateHandler{Templates: &mockTemplateStore{
		createFn: func(_ context.Context, _ *model.Template) (*model.Template, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/templates", h.CreateTemplate)

	body := jsonBody(t, model.Template{Name: "My Template"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/templates", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreateTemplate store error: want 500, got %d", w.Code)
	}
}

// ─── OrgHandler tests ────────────────────────────────────────────────────────

func TestOrgHandler_ListOrgs_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &OrgHandler{
		Orgs: &mockOrgStore{
			listForUserFn: func(_ uuid.UUID) ([]*model.OrgWithRole, error) {
				return nil, storeErr
			},
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/orgs", h.ListOrgs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orgs", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListOrgs store error: want 500, got %d", w.Code)
	}
}

func TestOrgHandler_CreateOrg_EmptyName_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: &mockOrgStore{}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs", h.CreateOrg)

	body := jsonBody(t, map[string]string{"name": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateOrg empty name: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_CreateOrg_CreateError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &OrgHandler{
		Orgs: &mockOrgStore{
			createOrgFn: func(_ *model.Organization) (*model.Organization, error) {
				return nil, storeErr
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreateOrg create error: want 500, got %d", w.Code)
	}
}

func TestOrgHandler_CreateOrg_AddMemberError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	created := &model.Organization{ID: uuid.New(), Name: "My Org"}
	h := &OrgHandler{
		Orgs: &mockOrgStore{
			createOrgFn: func(_ *model.Organization) (*model.Organization, error) {
				return created, nil
			},
			addMemberFn: func(_, _ uuid.UUID, _ model.OrgRole) (*model.OrgMember, error) {
				return nil, storeErr
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreateOrg addMember error: want 500, got %d", w.Code)
	}
}

// ─── mockUserStore ────────────────────────────────────────────────────────────

type mockUserStore struct {
	getByIDFn    func(id uuid.UUID) (*model.User, error)
	getByEmailFn func(email string) (*model.User, error)
	searchFn     func(query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error)
}

func (m *mockUserStore) Upsert(_ context.Context, sub, issuer string, email, name *string) (*model.User, error) {
	return nil, nil
}
func (m *mockUserStore) GetByID(_ context.Context, id uuid.UUID) (*model.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return nil, store.ErrNotFound
}
func (m *mockUserStore) GetByEmail(_ context.Context, email string) (*model.User, error) {
	if m.getByEmailFn != nil {
		return m.getByEmailFn(email)
	}
	return nil, store.ErrNotFound
}
func (m *mockUserStore) Search(_ context.Context, query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error) {
	if m.searchFn != nil {
		return m.searchFn(query, excludeID, orgIDs, limit)
	}
	return nil, nil
}

// ownerOrgStore returns a mockOrgStore that passes orgOwnerGuard (the calling user is always an owner).
func ownerOrgStore(overrides ...*mockOrgStore) *mockOrgStore {
	base := &mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
		},
	}
	if len(overrides) > 0 && overrides[0] != nil {
		o := overrides[0]
		base.listForUserFn = o.listForUserFn
		base.createOrgFn = o.createOrgFn
		base.addMemberFn = o.addMemberFn
		base.deleteFn = o.deleteFn
		base.getByIDFn = o.getByIDFn
		base.listMembersFn = o.listMembersFn
		base.updateOrgFn = o.updateOrgFn
		base.updateMemberRoleFn = o.updateMemberRoleFn
		base.removeMemberFn = o.removeMemberFn
		if o.getMemberFn != nil {
			base.getMemberFn = o.getMemberFn
		}
	}
	return base
}

// ─── OrgHandler additional tests ──────────────────────────────────────────────

func TestOrgHandler_DeleteOrg_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
		deleteFn: func(_ uuid.UUID) error { return storeErr },
	})}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId", h.DeleteOrg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("DeleteOrg store error: want 500, got %d", w.Code)
	}
}

func TestOrgHandler_GetOrg_ListMembersError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	orgID := uuid.New()
	h := &OrgHandler{Orgs: &mockOrgStore{
		// GetMember called twice in GetOrg: once for membership check, once (optionally) — use a counter
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
		},
		getByIDFn: func(id uuid.UUID) (*model.Organization, error) {
			return &model.Organization{ID: id, Name: "Org"}, nil
		},
		listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/orgs/:orgId", h.GetOrg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orgs/"+orgID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("GetOrg listMembers error: want 500, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrg_EmptyName_Returns400(t *testing.T) {
	h := &OrgHandler{Orgs: ownerOrgStore(nil)}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId", h.UpdateOrg)

	body := jsonBody(t, map[string]string{"name": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrg empty name: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrg_UpdateError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	orgID := uuid.New()
	h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
		getByIDFn: func(id uuid.UUID) (*model.Organization, error) {
			return &model.Organization{ID: id, Name: "Old"}, nil
		},
		updateOrgFn: func(_ *model.Organization) (*model.Organization, error) {
			return nil, storeErr
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdateOrg update error: want 500, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_EmptyUserIDAndEmail_Returns400(t *testing.T) {
	h := &OrgHandler{
		Orgs:  ownerOrgStore(nil),
		Users: &mockUserStore{},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	body := jsonBody(t, map[string]string{"userId": "", "email": ""})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("AddOrgMember empty ids: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_InvalidUserID_Returns400(t *testing.T) {
	h := &OrgHandler{
		Orgs:  ownerOrgStore(nil),
		Users: &mockUserStore{},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	body := jsonBody(t, map[string]string{"userId": "not-a-uuid"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("AddOrgMember invalid userId: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_UserNotFound_Returns404(t *testing.T) {
	h := &OrgHandler{
		Orgs: ownerOrgStore(nil),
		Users: &mockUserStore{
			getByIDFn: func(_ uuid.UUID) (*model.User, error) { return nil, store.ErrNotFound },
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	body := jsonBody(t, map[string]string{"userId": uuid.New().String()})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("AddOrgMember user not found: want 404, got %d", w.Code)
	}
}

func TestOrgHandler_AddOrgMember_ByEmail_UserNotFound_Returns404(t *testing.T) {
	h := &OrgHandler{
		Orgs: ownerOrgStore(nil),
		Users: &mockUserStore{
			getByEmailFn: func(_ string) (*model.User, error) { return nil, store.ErrNotFound },
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/orgs/:orgId/members", h.AddOrgMember)

	body := jsonBody(t, map[string]string{"email": "nobody@example.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs/"+uuid.New().String()+"/members", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("AddOrgMember email not found: want 404, got %d", w.Code)
	}
}

func TestOrgHandler_countOrgOwners_ListMembersError(t *testing.T) {
	// countOrgOwners is triggered from UpdateOrgMemberRole when demoting an existing owner.
	storeErr := errors.New("db failure")
	memberID := uuid.New()
	h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			// First call (orgOwnerGuard) — caller is owner.
			// Second call (GetMember in UpdateOrgMemberRole) — member being demoted is also owner.
			return &model.OrgMember{UserID: userID, OrgID: uuid.New(), Role: model.OrgRoleOwner}, nil
		},
		listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
			return nil, storeErr
		},
	})}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	// Demote the member to viewer — this triggers the countOrgOwners path
	body := jsonBody(t, map[string]string{"role": "viewer"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("countOrgOwners listMembers error: want 500, got %d", w.Code)
	}
}

func TestOrgHandler_UpdateOrgMemberRole_LastOwner_Returns400(t *testing.T) {
	memberID := uuid.New()
	h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			return &model.OrgMember{UserID: userID, OrgID: uuid.New(), Role: model.OrgRoleOwner}, nil
		},
		listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
			// Return just 1 owner
			return []*model.OrgMember{{Role: model.OrgRoleOwner}}, nil
		},
	})}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	body := jsonBody(t, map[string]string{"role": "viewer"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateOrgMemberRole last owner: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_RemoveOrgMember_MemberNotFound_Returns404(t *testing.T) {
	memberID := uuid.New()
	h := &OrgHandler{Orgs: &mockOrgStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
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
		t.Errorf("RemoveOrgMember not found: want 404, got %d", w.Code)
	}
}

func TestOrgHandler_RemoveOrgMember_LastOwner_Returns400(t *testing.T) {
	memberID := uuid.New()
	h := &OrgHandler{Orgs: &mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			// Member exists and is an owner
			return &model.OrgMember{UserID: userID, OrgID: uuid.New(), Role: model.OrgRoleOwner}, nil
		},
		listMembersFn: func(_ uuid.UUID) ([]*model.OrgMember, error) {
			return []*model.OrgMember{{Role: model.OrgRoleOwner}}, nil
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)

	w := httptest.NewRecorder()
	// Use different memberID than testUser's ID to ensure it goes through orgOwnerGuard
	req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("RemoveOrgMember last owner: want 400, got %d", w.Code)
	}
}

// ─── SearchUsers tests ────────────────────────────────────────────────────────

func TestSearchUsers_EmptyQuery_ReturnsEmptySlice(t *testing.T) {
	h := &ShareHandler{
		Orgs:  &mockOrgStore{},
		Users: &mockUserStore{},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/users/search", h.SearchUsers)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/search?q=", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("SearchUsers empty q: want 200, got %d", w.Code)
	}
}

func TestSearchUsers_GetUserOrgIDsError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &ShareHandler{
		Orgs: &mockOrgStore{
			getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
				return nil, store.ErrNotFound
			},
		},
		Users: &mockUserStore{},
	}
	// Override GetUserOrgIDs via a custom type (mockOrgStore doesn't have a field for it yet).
	// Use a wrapper that has GetUserOrgIDs returning error.
	type orgStoreWithGetUserOrgIDs interface {
		store.OrgStore
	}
	_ = orgStoreWithGetUserOrgIDs(nil)

	h2 := &ShareHandler{
		Orgs:  &orgStoreGetUserOrgIDsError{mockOrgStore: &mockOrgStore{}, err: storeErr},
		Users: &mockUserStore{},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/users/search", h2.SearchUsers)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/search?q=alice", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("SearchUsers orgIDs error: want 500, got %d", w.Code)
	}

	_ = h
}

// orgStoreGetUserOrgIDsError wraps mockOrgStore and overrides GetUserOrgIDs to return an error.
type orgStoreGetUserOrgIDsError struct {
	*mockOrgStore
	err error
}

func (o *orgStoreGetUserOrgIDsError) GetUserOrgIDs(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
	return nil, o.err
}

// ─── mockTaskStore ────────────────────────────────────────────────────────────

type mockTaskStore struct {
	listByUserFn          func(ctx context.Context, userID uuid.UUID) ([]*model.Task, error)
	listByUserPaginatedFn func(ctx context.Context, userID uuid.UUID, pg store.Pagination) ([]*model.Task, int, error)
	listBySourceNodeFn    func(ctx context.Context, userID uuid.UUID, sourceNodeID string) ([]*model.Task, error)
	listBySourcePageFn    func(ctx context.Context, userID uuid.UUID, pageID uuid.UUID) ([]*model.Task, error)
	listByFilterFn        func(ctx context.Context, userID uuid.UUID, fs model.FilterSet) ([]*model.Task, error)
	createFn              func(ctx context.Context, t *model.Task) (*model.Task, error)
	getByIDFn             func(id, userID uuid.UUID) (*model.Task, error)
	updateFn              func(t *model.Task) (*model.Task, error)
	upsertFn              func(t *model.Task) (*model.Task, error)
	deleteFn              func(id, userID uuid.UUID) error
}

func (m *mockTaskStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Task, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockTaskStore) ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg store.Pagination) ([]*model.Task, int, error) {
	if m.listByUserPaginatedFn != nil {
		return m.listByUserPaginatedFn(ctx, userID, pg)
	}
	return nil, 0, nil
}
func (m *mockTaskStore) ListBySourcePage(ctx context.Context, userID uuid.UUID, pageID uuid.UUID) ([]*model.Task, error) {
	if m.listBySourcePageFn != nil {
		return m.listBySourcePageFn(ctx, userID, pageID)
	}
	return nil, nil
}
func (m *mockTaskStore) ListBySourceNode(ctx context.Context, userID uuid.UUID, sourceNodeID string) ([]*model.Task, error) {
	if m.listBySourceNodeFn != nil {
		return m.listBySourceNodeFn(ctx, userID, sourceNodeID)
	}
	return nil, nil
}
func (m *mockTaskStore) ListByFilter(ctx context.Context, userID uuid.UUID, fs model.FilterSet) ([]*model.Task, error) {
	if m.listByFilterFn != nil {
		return m.listByFilterFn(ctx, userID, fs)
	}
	return nil, nil
}
func (m *mockTaskStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Task, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id, userID)
	}
	return nil, store.ErrNotFound
}
func (m *mockTaskStore) Create(ctx context.Context, t *model.Task) (*model.Task, error) {
	if m.createFn != nil {
		return m.createFn(ctx, t)
	}
	return t, nil
}
func (m *mockTaskStore) Update(_ context.Context, t *model.Task) (*model.Task, error) {
	if m.updateFn != nil {
		return m.updateFn(t)
	}
	return t, nil
}
func (m *mockTaskStore) Upsert(_ context.Context, t *model.Task) (*model.Task, error) {
	if m.upsertFn != nil {
		return m.upsertFn(t)
	}
	return t, nil
}
func (m *mockTaskStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id, userID)
	}
	return nil
}
func (m *mockTaskStore) ListByFolder(_ context.Context, folderID uuid.UUID, descendantPageIDs []uuid.UUID) ([]*model.Task, error) {
	return nil, nil
}

// ─── TaskHandler tests ────────────────────────────────────────────────────────

func TestTaskHandler_ListTasks_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TaskHandler{Tasks: &mockTaskStore{
		listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Task, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks", h.ListTasks)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListTasks store error: want 500, got %d", w.Code)
	}
}

func TestTaskHandler_ListTasks_SourceNodeError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TaskHandler{Tasks: &mockTaskStore{
		listBySourceNodeFn: func(_ context.Context, _ uuid.UUID, _ string) ([]*model.Task, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks", h.ListTasks)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks?sourceNodeId=abc-node", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListTasks sourceNodeId error: want 500, got %d", w.Code)
	}
}

func TestTaskHandler_ListTasks_SourcePageError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TaskHandler{Tasks: &mockTaskStore{
		listBySourcePageFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]*model.Task, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/tasks", h.ListTasks)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks?sourcePageId="+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListTasks sourcePageId error: want 500, got %d", w.Code)
	}
}

func TestTaskHandler_CreateTask_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TaskHandler{Tasks: &mockTaskStore{
		createFn: func(_ context.Context, _ *model.Task) (*model.Task, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/tasks", h.CreateTask)

	body := jsonBody(t, model.Task{Title: "My Task"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreateTask store error: want 500, got %d", w.Code)
	}
}

func TestTaskHandler_FilterTasks_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &TaskHandler{Tasks: &mockTaskStore{
		listByFilterFn: func(_ context.Context, _ uuid.UUID, _ model.FilterSet) ([]*model.Task, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/tasks/filter", h.FilterTasks)

	body := jsonBody(t, model.FilterSet{Conjunction: "and", Rules: []model.FilterRule{}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks/filter", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("FilterTasks store error: want 500, got %d", w.Code)
	}
}

// ─── mockPageStore ────────────────────────────────────────────────────────────

type mockPageStore struct {
	listByUserFn          func(ctx context.Context, userID uuid.UUID) ([]*model.Page, error)
	listByUserPaginatedFn func(ctx context.Context, userID uuid.UUID, pg store.Pagination) ([]*model.Page, int, error)
	createFn              func(ctx context.Context, p *model.Page) (*model.Page, error)
	getByIDFn             func(id, userID uuid.UUID) (*model.Page, error)
	upsertContentFn       func(pc *model.PageContent, userID uuid.UUID) (*model.PageContent, error)
	getContentFn          func(pageID, userID uuid.UUID) (*model.PageContent, error)
	updateFn              func(p *model.Page) (*model.Page, error)
	deleteFn              func(id, userID uuid.UUID) error
	isAncestorFn          func(candidateAncestorID, nodeID uuid.UUID) (bool, error)
	upsertFn              func(p *model.Page) (*model.Page, error)
}

func (m *mockPageStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Page, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockPageStore) ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg store.Pagination) ([]*model.Page, int, error) {
	if m.listByUserPaginatedFn != nil {
		return m.listByUserPaginatedFn(ctx, userID, pg)
	}
	return nil, 0, nil
}
func (m *mockPageStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Page, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id, userID)
	}
	return nil, store.ErrNotFound
}
func (m *mockPageStore) GetFolderByID(_ context.Context, id, userID uuid.UUID) (*model.Page, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id, userID)
	}
	return nil, store.ErrNotFound
}
func (m *mockPageStore) Create(ctx context.Context, p *model.Page) (*model.Page, error) {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return p, nil
}
func (m *mockPageStore) Update(_ context.Context, p *model.Page) (*model.Page, error) {
	if m.updateFn != nil {
		return m.updateFn(p)
	}
	return p, nil
}
func (m *mockPageStore) Upsert(_ context.Context, p *model.Page) (*model.Page, error) {
	if m.upsertFn != nil {
		return m.upsertFn(p)
	}
	return p, nil
}
func (m *mockPageStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id, userID)
	}
	return nil
}
func (m *mockPageStore) GetContent(_ context.Context, pageID, userID uuid.UUID) (*model.PageContent, error) {
	if m.getContentFn != nil {
		return m.getContentFn(pageID, userID)
	}
	return nil, store.ErrNotFound
}
func (m *mockPageStore) UpsertContent(_ context.Context, pc *model.PageContent, userID uuid.UUID) (*model.PageContent, error) {
	if m.upsertContentFn != nil {
		return m.upsertContentFn(pc, userID)
	}
	return pc, nil
}
func (m *mockPageStore) IsAncestor(_ context.Context, candidateAncestorID, nodeID uuid.UUID) (bool, error) {
	if m.isAncestorFn != nil {
		return m.isAncestorFn(candidateAncestorID, nodeID)
	}
	return false, nil
}
func (m *mockPageStore) GetDescendantIDs(_ context.Context, folderID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

// ─── PageHandler tests ────────────────────────────────────────────────────────

func TestPageHandler_ListPages_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &PageHandler{Pages: &mockPageStore{
		listByUserFn: func(_ context.Context, _ uuid.UUID) ([]*model.Page, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.GET("/pages", h.ListPages)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pages", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("ListPages store error: want 500, got %d", w.Code)
	}
}

func TestPageHandler_CreatePage_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	h := &PageHandler{Pages: &mockPageStore{
		createFn: func(_ context.Context, _ *model.Page) (*model.Page, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.POST("/pages", h.CreatePage)

	body := jsonBody(t, model.Page{Title: "My Page"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pages", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("CreatePage store error: want 500, got %d", w.Code)
	}
}

// ─── UpdateOrgMemberRole additional tests ─────────────────────────────────────

func TestOrgHandler_UpdateOrgMemberRole_MemberNotFound_Returns404(t *testing.T) {
	callCount := 0
	h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			callCount++
			if callCount == 1 {
				// orgOwnerGuard: caller is owner
				return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
			}
			// Second call (GetMember for the member being changed): not found
			return nil, store.ErrNotFound
		},
	})}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	body := jsonBody(t, map[string]string{"role": "viewer"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+uuid.New().String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpdateOrgMemberRole member not found: want 404, got %d", w.Code)
	}
}

func TestOrgHandler_RemoveOrgMember_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	// Use testUser's own ID as memberID so orgOwnerGuard is skipped (self-leave path).
	memberID := testUser().ID
	h := &OrgHandler{Orgs: &mockOrgStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
			// The member being removed is a viewer (no last-owner protection needed).
			return &model.OrgMember{UserID: memberID, OrgID: uuid.New(), Role: model.OrgRoleViewer}, nil
		},
		removeMemberFn: func(_, _ uuid.UUID) error { return storeErr },
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/orgs/:orgId/members/:userId", h.RemoveOrgMember)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("RemoveOrgMember store error: want 500, got %d", w.Code)
	}
}

// ─── ShareHandler / isResourceOwner tests ─────────────────────────────────────

func newShareHandlerWithTaskResource(t *testing.T, taskOwnerID uuid.UUID, shareDeleteErr error) (*ShareHandler, uuid.UUID) {
	t.Helper()
	taskID := uuid.New()
	return &ShareHandler{
		Shares: &mockShareStore{
			getByIDShareFn: func(id uuid.UUID) (*model.Share, error) {
				return &model.Share{
					ResourceType: model.ShareResourceTask,
					ResourceID:   taskID,
				}, nil
			},
			deleteShareFn: func(_ uuid.UUID) error { return shareDeleteErr },
		},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, userID uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: taskID, UserID: taskOwnerID}, nil
			},
		},
	}, taskID
}

func TestShareHandler_isResourceOwner_Task_OwnerMismatch_Returns403(t *testing.T) {
	otherUserID := uuid.New()
	shareID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{
			getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
				return &model.Share{
					ResourceType: model.ShareResourceTask,
					ResourceID:   uuid.New(),
				}, nil
			},
		},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, userID uuid.UUID) (*model.Task, error) {
				// Task is owned by a different user
				return &model.Task{ID: id, UserID: otherUserID}, nil
			},
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/shares/:shareId", h.DeleteShare)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+shareID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("isResourceOwner task mismatch: want 403, got %d", w.Code)
	}
}

func TestShareHandler_isResourceOwner_Template_OwnerMatch_Returns204(t *testing.T) {
	callerID := testUser().ID
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
			getByIDFn: func(id, userID uuid.UUID) (*model.Template, error) {
				return &model.Template{ID: id, UserID: callerID}, nil
			},
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/shares/:shareId", h.DeleteShare)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+shareID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("isResourceOwner template owner: want 204, got %d", w.Code)
	}
}

func TestShareHandler_DeleteShare_GetByIDError_Returns404(t *testing.T) {
	shareID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{
			// default getByIDShareFn returns ErrNotFound
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/shares/:shareId", h.DeleteShare)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+shareID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("DeleteShare not found: want 404, got %d", w.Code)
	}
}

func TestShareHandler_DeleteShare_StoreDeleteError_Returns500(t *testing.T) {
	storeErr := errors.New("delete failed")
	callerID := testUser().ID
	h, _ := newShareHandlerWithTaskResource(t, callerID, storeErr)

	shareID := uuid.New()
	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/shares/:shareId", h.DeleteShare)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+shareID.String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("DeleteShare store error: want 500, got %d", w.Code)
	}
}

func TestShareHandler_UpdateSharePermission_GetByIDError_Returns404(t *testing.T) {
	shareID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/shares/:shareId", h.UpdateSharePermission)

	body := jsonBody(t, map[string]string{"permission": "viewer"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/shares/"+shareID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("UpdateSharePermission not found: want 404, got %d", w.Code)
	}
}

func TestShareHandler_UpdateSharePermission_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("update failed")
	callerID := testUser().ID
	shareID := uuid.New()
	h := &ShareHandler{
		Shares: &mockShareStore{
			getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
				return &model.Share{
					ResourceType: model.ShareResourceTask,
					ResourceID:   uuid.New(),
				}, nil
			},
			updatePermissionFn: func(_ uuid.UUID, _ model.SharePermission) (*model.Share, error) {
				return nil, storeErr
			},
		},
		Tasks: &mockTaskStore{
			getByIDFn: func(id, userID uuid.UUID) (*model.Task, error) {
				return &model.Task{ID: id, UserID: callerID}, nil
			},
		},
	}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/shares/:shareId", h.UpdateSharePermission)

	body := jsonBody(t, map[string]string{"permission": "editor"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/shares/"+shareID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdateSharePermission store error: want 500, got %d", w.Code)
	}
}

// ─── PageHandler additional tests ─────────────────────────────────────────────

func testPage(userID uuid.UUID) *model.Page {
	return &model.Page{ID: uuid.New(), UserID: userID, Title: "Test Page", Type: model.NodeTypePage, Tags: []string{}}
}

func TestPageHandler_DeletePage_NonOwner_Returns403(t *testing.T) {
	otherUserID := uuid.New()
	h := &PageHandler{Pages: &mockPageStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
			// Page is owned by someone else
			return testPage(otherUserID), nil
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.DELETE("/pages/:id", h.DeletePage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/pages/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("DeletePage non-owner: want 403, got %d", w.Code)
	}
}

func TestPageHandler_UpsertPageContent_UpsertError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	callerID := testUser().ID
	pageID := uuid.New()
	h := &PageHandler{Pages: &mockPageStore{
		getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
			return &model.Page{ID: pageID, UserID: callerID, Tags: []string{}}, nil
		},
		upsertContentFn: func(_ *model.PageContent, _ uuid.UUID) (*model.PageContent, error) {
			return nil, storeErr
		},
	}}

	r := gin.New()
	r.Use(injectTestUser())
	r.PUT("/pages/:id/content", h.UpsertPageContent)

	// Send an empty PageContent body (no content field) so the validation check is skipped
	// and execution reaches UpsertContent directly.
	body := jsonBody(t, map[string]any{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/pages/"+pageID.String()+"/content", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpsertPageContent store error: want 500, got %d", w.Code)
	}
}

// ─── UpdateOrgMemberRole additional tests ─────────────────────────────────────

func TestOrgHandler_UpdateOrgMemberRole_StoreError_Returns500(t *testing.T) {
	storeErr := errors.New("db failure")
	memberID := uuid.New()
	h := &OrgHandler{Orgs: ownerOrgStore(&mockOrgStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
			// Always return owner so both orgOwnerGuard and GetMember in the function return owner
			return &model.OrgMember{UserID: userID, Role: model.OrgRoleOwner}, nil
		},
		updateMemberRoleFn: func(_, _ uuid.UUID, _ model.OrgRole) (*model.OrgMember, error) {
			return nil, storeErr
		},
	})}

	r := gin.New()
	r.Use(injectTestUser())
	r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

	// Promote to owner (role == OrgRoleOwner skips the demote-guard check)
	body := jsonBody(t, map[string]string{"role": "owner"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/orgs/"+uuid.New().String()+"/members/"+memberID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("UpdateOrgMemberRole store error: want 500, got %d", w.Code)
	}
}
