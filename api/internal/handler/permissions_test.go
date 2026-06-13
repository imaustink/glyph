package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ─── Minimal mock store implementations ──────────────────────────────────────

type mockOrgStore struct {
	getMemberFn        func(ctx context.Context, orgID, userID uuid.UUID) (*model.OrgMember, error)
	listForUserFn      func(userID uuid.UUID) ([]*model.OrgWithRole, error)
	createOrgFn        func(org *model.Organization) (*model.Organization, error)
	addMemberFn        func(orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error)
	deleteFn           func(id uuid.UUID) error
	getByIDFn          func(id uuid.UUID) (*model.Organization, error)
	listMembersFn      func(orgID uuid.UUID) ([]*model.OrgMember, error)
	updateOrgFn        func(org *model.Organization) (*model.Organization, error)
	updateMemberRoleFn func(orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error)
	removeMemberFn     func(orgID, userID uuid.UUID) error
}

func (m *mockOrgStore) GetMember(ctx context.Context, orgID, userID uuid.UUID) (*model.OrgMember, error) {
	if m.getMemberFn != nil {
		return m.getMemberFn(ctx, orgID, userID)
	}
	return nil, store.ErrNotFound
}

func (m *mockOrgStore) Create(_ context.Context, org *model.Organization) (*model.Organization, error) {
	if m.createOrgFn != nil {
		return m.createOrgFn(org)
	}
	return nil, nil
}
func (m *mockOrgStore) GetByID(_ context.Context, id uuid.UUID) (*model.Organization, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return nil, nil
}
func (m *mockOrgStore) ListForUser(_ context.Context, userID uuid.UUID) ([]*model.OrgWithRole, error) {
	if m.listForUserFn != nil {
		return m.listForUserFn(userID)
	}
	return nil, nil
}
func (m *mockOrgStore) Update(_ context.Context, org *model.Organization) (*model.Organization, error) {
	if m.updateOrgFn != nil {
		return m.updateOrgFn(org)
	}
	return org, nil
}
func (m *mockOrgStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	return nil
}
func (m *mockOrgStore) AddMember(_ context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
	if m.addMemberFn != nil {
		return m.addMemberFn(orgID, userID, role)
	}
	return nil, nil
}
func (m *mockOrgStore) ListMembers(_ context.Context, orgID uuid.UUID) ([]*model.OrgMember, error) {
	if m.listMembersFn != nil {
		return m.listMembersFn(orgID)
	}
	return nil, nil
}
func (m *mockOrgStore) UpdateMemberRole(_ context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
	if m.updateMemberRoleFn != nil {
		return m.updateMemberRoleFn(orgID, userID, role)
	}
	return &model.OrgMember{UserID: userID, OrgID: orgID, Role: role}, nil
}
func (m *mockOrgStore) RemoveMember(_ context.Context, orgID, userID uuid.UUID) error {
	if m.removeMemberFn != nil {
		return m.removeMemberFn(orgID, userID)
	}
	return nil
}
func (m *mockOrgStore) GetUserOrgIDs(_ context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

type mockShareStore struct {
	getForUserAndResourceFn func(ctx context.Context, userID uuid.UUID, rt model.ShareResourceType, rid uuid.UUID) (*model.Share, error)
	getByIDShareFn          func(id uuid.UUID) (*model.Share, error)
	listForResourceFn       func(rt model.ShareResourceType, rid uuid.UUID) ([]*model.Share, error)
	updatePermissionFn      func(id uuid.UUID, permission model.SharePermission) (*model.Share, error)
	deleteShareFn           func(id uuid.UUID) error
	createShareFn           func(s *model.Share) (*model.Share, error)
}

func (m *mockShareStore) GetForUserAndResource(ctx context.Context, userID uuid.UUID, rt model.ShareResourceType, rid uuid.UUID) (*model.Share, error) {
	if m.getForUserAndResourceFn != nil {
		return m.getForUserAndResourceFn(ctx, userID, rt, rid)
	}
	return nil, store.ErrNotFound
}

func (m *mockShareStore) Create(_ context.Context, s *model.Share) (*model.Share, error) {
	if m.createShareFn != nil {
		return m.createShareFn(s)
	}
	return nil, nil
}
func (m *mockShareStore) GetByID(_ context.Context, id uuid.UUID) (*model.Share, error) {
	if m.getByIDShareFn != nil {
		return m.getByIDShareFn(id)
	}
	return nil, store.ErrNotFound
}
func (m *mockShareStore) ListForResource(_ context.Context, resourceType model.ShareResourceType, resourceID uuid.UUID) ([]*model.Share, error) {
	if m.listForResourceFn != nil {
		return m.listForResourceFn(resourceType, resourceID)
	}
	return nil, nil
}
func (m *mockShareStore) UpdatePermission(_ context.Context, id uuid.UUID, permission model.SharePermission) (*model.Share, error) {
	if m.updatePermissionFn != nil {
		return m.updatePermissionFn(id, permission)
	}
	return nil, nil
}
func (m *mockShareStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.deleteShareFn != nil {
		return m.deleteShareFn(id)
	}
	return nil
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

func newTestGinContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

// ─── CanWriteResource tests ───────────────────────────────────────────────────

func TestCanWriteResource_OwnerAlwaysAllowed(t *testing.T) {
	ownerID := uuid.New()
	pc := &PermissionChecker{}
	c, _ := newTestGinContext()

	if !pc.CanWriteResource(c, ownerID, nil, model.ShareResourcePage, uuid.New(), ownerID) {
		t.Error("owner should always have write access")
	}
}

func TestCanWriteResource_OrgOwnerAllowed(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()
	orgID := uuid.New()

	pc := &PermissionChecker{
		Orgs: &mockOrgStore{
			getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
				return &model.OrgMember{Role: model.OrgRoleOwner}, nil
			},
		},
	}
	c, _ := newTestGinContext()

	if !pc.CanWriteResource(c, ownerID, &orgID, model.ShareResourcePage, uuid.New(), requesterID) {
		t.Error("org owner should have write access")
	}
}

func TestCanWriteResource_OrgEditorAllowed(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()
	orgID := uuid.New()

	pc := &PermissionChecker{
		Orgs: &mockOrgStore{
			getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
				return &model.OrgMember{Role: model.OrgRoleEditor}, nil
			},
		},
	}
	c, _ := newTestGinContext()

	if !pc.CanWriteResource(c, ownerID, &orgID, model.ShareResourcePage, uuid.New(), requesterID) {
		t.Error("org editor should have write access")
	}
}

func TestCanWriteResource_OrgViewerDenied(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()
	orgID := uuid.New()

	pc := &PermissionChecker{
		Orgs: &mockOrgStore{
			getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
				return &model.OrgMember{Role: model.OrgRoleViewer}, nil
			},
		},
		Shares: &mockShareStore{}, // no share
	}
	c, w := newTestGinContext()

	if pc.CanWriteResource(c, ownerID, &orgID, model.ShareResourcePage, uuid.New(), requesterID) {
		t.Error("org viewer should not have write access")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
}

func TestCanWriteResource_DirectShareEditorAllowed(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()
	resourceID := uuid.New()

	pc := &PermissionChecker{
		Shares: &mockShareStore{
			getForUserAndResourceFn: func(_ context.Context, _ uuid.UUID, _ model.ShareResourceType, _ uuid.UUID) (*model.Share, error) {
				return &model.Share{Permission: model.SharePermissionEditor}, nil
			},
		},
	}
	c, _ := newTestGinContext()

	if !pc.CanWriteResource(c, ownerID, nil, model.ShareResourcePage, resourceID, requesterID) {
		t.Error("direct editor share should grant write access")
	}
}

func TestCanWriteResource_DirectShareViewerDenied(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()
	resourceID := uuid.New()

	pc := &PermissionChecker{
		Shares: &mockShareStore{
			getForUserAndResourceFn: func(_ context.Context, _ uuid.UUID, _ model.ShareResourceType, _ uuid.UUID) (*model.Share, error) {
				return &model.Share{Permission: model.SharePermissionViewer}, nil
			},
		},
	}
	c, w := newTestGinContext()

	if pc.CanWriteResource(c, ownerID, nil, model.ShareResourcePage, resourceID, requesterID) {
		t.Error("viewer share should not grant write access")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
}

func TestCanWriteResource_NoAccessDenied(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()

	pc := &PermissionChecker{
		Shares: &mockShareStore{}, // returns ErrNotFound
	}
	c, w := newTestGinContext()

	if pc.CanWriteResource(c, ownerID, nil, model.ShareResourcePage, uuid.New(), requesterID) {
		t.Error("user with no access should be denied")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
}

func TestCanWriteResource_OrgStoreErrorReturns500(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()
	orgID := uuid.New()

	pc := &PermissionChecker{
		Orgs: &mockOrgStore{
			getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.OrgMember, error) {
				return nil, errors.New("database connection lost")
			},
		},
	}
	c, w := newTestGinContext()

	if pc.CanWriteResource(c, ownerID, &orgID, model.ShareResourcePage, uuid.New(), requesterID) {
		t.Error("store error should cause denial")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestCanWriteResource_ShareStoreErrorReturns500(t *testing.T) {
	ownerID := uuid.New()
	requesterID := uuid.New()

	pc := &PermissionChecker{
		Shares: &mockShareStore{
			getForUserAndResourceFn: func(_ context.Context, _ uuid.UUID, _ model.ShareResourceType, _ uuid.UUID) (*model.Share, error) {
				return nil, errors.New("db error")
			},
		},
	}
	c, w := newTestGinContext()

	if pc.CanWriteResource(c, ownerID, nil, model.ShareResourcePage, uuid.New(), requesterID) {
		t.Error("store error should cause denial")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestCanWritePage_DelegatesToCanWriteResource(t *testing.T) {
	ownerID := uuid.New()
	page := &model.Page{
		ID:     uuid.New(),
		UserID: ownerID,
	}
	pc := &PermissionChecker{}
	c, _ := newTestGinContext()

	// Owner of the page can always write it
	if !pc.CanWritePage(c, page, ownerID) {
		t.Error("owner should be able to write their own page")
	}
}
