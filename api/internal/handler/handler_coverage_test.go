package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// ─── ListPages: negative int coverage (distinct from "abc" parse error) ──────

func TestPageHandler_ListPages_NegativeLimit_Returns400(t *testing.T) {
h := &PageHandler{Pages: &mockPageStore{}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/pages", h.ListPages)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/pages?limit=-1", nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("negative limit: want 400, got %d", w.Code)
}
}

func TestPageHandler_ListPages_NegativeOffset_Returns400(t *testing.T) {
h := &PageHandler{Pages: &mockPageStore{}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/pages", h.ListPages)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/pages?offset=-1", nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("negative offset: want 400, got %d", w.Code)
}
}

func TestPageHandler_ListPages_PaginatedStoreError_Returns500(t *testing.T) {
storeErr := errors.New("db error")
h := &PageHandler{Pages: &mockPageStore{
listByUserPaginatedFn: func(_ context.Context, _ uuid.UUID, _ store.Pagination) ([]*model.Page, int, error) {
return nil, 0, storeErr
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/pages", h.ListPages)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/pages?limit=10&offset=0", nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusInternalServerError {
t.Errorf("paginated store error: want 500, got %d", w.Code)
}
}

func TestPageHandler_ListPages_PaginatedSuccess_Returns200(t *testing.T) {
pages := []*model.Page{{ID: uuid.New(), Title: "p1", Tags: []string{}}}
h := &PageHandler{Pages: &mockPageStore{
listByUserPaginatedFn: func(_ context.Context, _ uuid.UUID, _ store.Pagination) ([]*model.Page, int, error) {
return pages, 1, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.GET("/pages", h.ListPages)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodGet, "/pages?limit=10&offset=0", nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusOK {
t.Errorf("paginated success: want 200, got %d", w.Code)
}
if w.Header().Get("X-Total-Count") != "1" {
t.Errorf("X-Total-Count: want 1, got %s", w.Header().Get("X-Total-Count"))
}
}

// ─── isResourceOwner coverage ──────────────────────────────────────────────────

// Success path: page found and caller is the owner.
func TestShareHandler_isResourceOwner_Page_Success_Returns204(t *testing.T) {
callerID := testUser().ID
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
return &model.Share{
ResourceType: model.ShareResourcePage,
ResourceID:   uuid.New(),
}, nil
},
deleteShareFn: func(_ uuid.UUID) error { return nil },
},
Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return testPage(callerID), nil
},
},
}

r := gin.New()
r.Use(injectTestUser())
r.DELETE("/shares/:shareId", h.DeleteShare)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/shares/"+uuid.New().String(), nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusNoContent {
t.Errorf("isResourceOwner page success: want 204, got %d", w.Code)
}
}

// Default case: unknown resource type hits the default branch.
func TestShareHandler_isResourceOwner_UnknownType_Returns400(t *testing.T) {
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
return &model.Share{
ResourceType: model.ShareResourceType("unknown"),
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
req := httptest.NewRequest(http.MethodDelete, "/shares/"+uuid.New().String(), nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("isResourceOwner unknown type: want 400, got %d", w.Code)
}
}

// Task GetByID error path.
func TestShareHandler_isResourceOwner_Task_NotFound_Returns404(t *testing.T) {
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
return nil, store.ErrNotFound
},
},
}

r := gin.New()
r.Use(injectTestUser())
r.DELETE("/shares/:shareId", h.DeleteShare)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/shares/"+uuid.New().String(), nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusNotFound {
t.Errorf("isResourceOwner task not found: want 404, got %d", w.Code)
}
}

// Template GetByID error path (name differs from TemplateGetByIDError test in extra file).
func TestShareHandler_isResourceOwner_Template_NotFound_Returns404(t *testing.T) {
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
return &model.Share{
ResourceType: model.ShareResourceTemplate,
ResourceID:   uuid.New(),
}, nil
},
},
Templates: &mockTemplateStore{
getByIDFn: func(id, userID uuid.UUID) (*model.Template, error) {
return nil, store.ErrNotFound
},
},
}

r := gin.New()
r.Use(injectTestUser())
r.DELETE("/shares/:shareId", h.DeleteShare)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/shares/"+uuid.New().String(), nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusNotFound {
t.Errorf("isResourceOwner template not found: want 404, got %d", w.Code)
}
}

// ─── UpdateOrgMemberRole: invalid userId UUID ─────────────────────────────────

func TestOrgHandler_UpdateOrgMemberRole_InvalidMemberID_Returns400(t *testing.T) {
h := &OrgHandler{Orgs: ownerOrgStore(nil)}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

w := httptest.NewRecorder()
req := httptest.NewRequest(
http.MethodPatch,
"/orgs/"+uuid.New().String()+"/members/not-a-uuid",
nil,
)
r.ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("invalid memberID: want 400, got %d", w.Code)
}
}

// ─── RegisterValidators: non-standard engine returns early ───────────────────

type fakeBindingValidator struct{}

func (f *fakeBindingValidator) ValidateStruct(obj any) error { return nil }
func (f *fakeBindingValidator) Engine() any                  { return "not-a-validator" }

func TestRegisterValidators_NonStandardEngine_ReturnsEarly(t *testing.T) {
orig := binding.Validator
binding.Validator = &fakeBindingValidator{}
defer func() { binding.Validator = orig }()

// Should return early without panic
RegisterValidators()
}


// ─── RegisterValidators: registration error triggers panic ───────────────────

func TestRegisterValidators_RegistrationError_Panics(t *testing.T) {
	for _, failAt := range []int{1, 2, 3, 4} {
		failAt := failAt
		t.Run(fmt.Sprintf("failAt%d", failAt), func(t *testing.T) {
			origReg := registerValidation
			callCount := 0
			registerValidation = func(v *validator.Validate, tag string, fn validator.Func, callValidationEvenIfNull ...bool) error {
				callCount++
				if callCount == failAt {
					return errors.New("injected error")
				}
				return v.RegisterValidation(tag, fn, callValidationEvenIfNull...)
			}
			defer func() { registerValidation = origReg }()

			defer func() {
				if r := recover(); r == nil {
					t.Errorf("failAt=%d: expected panic but none occurred", failAt)
				}
			}()
			RegisterValidators()
		})
	}
}

// ─── DeletePage: invalid page UUID ───────────────────────────────────────────

func TestPageHandler_DeletePage_InvalidID_Returns400(t *testing.T) {
h := &PageHandler{Pages: &mockPageStore{}}
r := gin.New()
r.Use(injectTestUser())
r.DELETE("/pages/:id", h.DeletePage)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodDelete, "/pages/not-a-uuid", nil)
r.ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("invalid page ID: want 400, got %d", w.Code)
}
}

// ─── UpdateSharePermission: invalid JSON body ─────────────────────────────────

func TestShareHandler_UpdateSharePermission_InvalidJSON_Returns400(t *testing.T) {
callerID := testUser().ID
h := &ShareHandler{
Shares: &mockShareStore{
getByIDShareFn: func(_ uuid.UUID) (*model.Share, error) {
return &model.Share{
ResourceType: model.ShareResourcePage,
ResourceID:   uuid.New(),
}, nil
},
},
Pages: &mockPageStore{
getByIDFn: func(id, _ uuid.UUID) (*model.Page, error) {
return testPage(callerID), nil
},
},
}

r := gin.New()
r.Use(injectTestUser())
r.PATCH("/shares/:shareId", h.UpdateSharePermission)

w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPatch, "/shares/"+uuid.New().String(), strings.NewReader("not-json"))
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("invalid JSON: want 400, got %d", w.Code)
}
}

// ─── UpdateOrgMemberRole: non-owner caller ────────────────────────────────────

func TestOrgHandler_UpdateOrgMemberRole_NonOwnerCaller_Returns403(t *testing.T) {
h := &OrgHandler{Orgs: &mockOrgStore{
getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.OrgMember, error) {
return &model.OrgMember{UserID: userID, Role: model.OrgRoleViewer}, nil
},
}}
r := gin.New()
r.Use(injectTestUser())
r.PATCH("/orgs/:orgId/members/:userId", h.UpdateOrgMemberRole)

body := jsonBody(t, map[string]string{"role": "editor"})
w := httptest.NewRecorder()
req := httptest.NewRequest(
http.MethodPatch,
"/orgs/"+uuid.New().String()+"/members/"+uuid.New().String(),
body,
)
req.Header.Set("Content-Type", "application/json")
r.ServeHTTP(w, req)

if w.Code != http.StatusForbidden {
t.Errorf("non-owner caller: want 403, got %d", w.Code)
}
}

// ─── unfurlCheckRedirect: 5-redirect limit ────────────────────────────────────

func TestUnfurlCheckRedirect_FiveRedirects_StopsFollowing(t *testing.T) {
// Build via[0..4] (5 elements) to trigger the >= 5 branch
via := make([]*http.Request, 5)
for i := range via {
via[i], _ = http.NewRequest(http.MethodGet, "http://example.com", nil)
}
err := unfurlCheckRedirect(nil, via)
if err != http.ErrUseLastResponse {
t.Errorf("want ErrUseLastResponse, got %v", err)
}
}

func TestUnfurlCheckRedirect_FewRedirects_Continues(t *testing.T) {
via := make([]*http.Request, 2)
for i := range via {
via[i], _ = http.NewRequest(http.MethodGet, "http://example.com", nil)
}
err := unfurlCheckRedirect(nil, via)
if err != nil {
t.Errorf("want nil, got %v", err)
}
}
