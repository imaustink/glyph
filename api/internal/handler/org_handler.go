package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// OrgHandler handles organization and member management.
type OrgHandler struct {
	Orgs  store.OrgStore
	Users store.UserStore
}

// ─── Orgs ─────────────────────────────────────────────────────────────────────

// GET /orgs
func (h *OrgHandler) ListOrgs(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireOrgReadScope(c) {
		return
	}
	orgs, err := h.Orgs.ListForUser(c.Request.Context(), user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, orgs)
}

// POST /orgs
func (h *OrgHandler) CreateOrg(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	org := &model.Organization{Name: body.Name, CreatedBy: user.ID}
	// The creator becomes the owner in the same transaction.
	created, err := h.Orgs.CreateWithOwner(c.Request.Context(), org)
	if err != nil {
		internalError(c, err)
		return
	}
	created.MemberCount = 1
	// Return OrgWithRole so the frontend knows the creator's role is "owner".
	c.JSON(http.StatusCreated, model.OrgWithRole{Organization: *created, Role: model.OrgRoleOwner})
}

// GET /orgs/:orgId
func (h *OrgHandler) GetOrg(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireOrgReadScope(c) {
		return
	}
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	// Must be a member to view.
	if _, err := h.Orgs.GetMember(c.Request.Context(), orgID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	org, err := h.Orgs.GetByID(c.Request.Context(), orgID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	members, err := h.Orgs.ListMembers(c.Request.Context(), orgID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"org": org, "members": members})
}

// PATCH /orgs/:orgId
func (h *OrgHandler) UpdateOrg(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !h.orgOwnerGuard(c, orgID, user.ID) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	org, err := h.Orgs.GetByID(c.Request.Context(), orgID)
	if err != nil {
		notFoundOrError(c, err)
		return
	}
	org.Name = body.Name
	updated, err := h.Orgs.Update(c.Request.Context(), org)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DELETE /orgs/:orgId
func (h *OrgHandler) DeleteOrg(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !h.orgOwnerGuard(c, orgID, user.ID) {
		return
	}
	if err := h.Orgs.Delete(c.Request.Context(), orgID); err != nil {
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── Org Members ──────────────────────────────────────────────────────────────

// POST /orgs/:orgId/members
func (h *OrgHandler) AddOrgMember(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !h.orgOwnerGuard(c, orgID, user.ID) {
		return
	}
	var body struct {
		UserID string        `json:"userId"`
		Email  string        `json:"email"`
		Role   model.OrgRole `json:"role"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if body.UserID == "" && body.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId or email is required"})
		return
	}
	if body.Role == "" {
		body.Role = model.OrgRoleViewer
	}
	if !body.Role.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	var memberID uuid.UUID
	if body.UserID != "" {
		var err error
		memberID, err = uuid.Parse(body.UserID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid userId"})
			return
		}
		// Verify target user exists.
		if _, err := h.Users.GetByID(c.Request.Context(), memberID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	} else {
		u, err := h.Users.GetByEmail(c.Request.Context(), body.Email)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no user found with that email"})
			return
		}
		memberID = u.ID
	}
	member, err := h.Orgs.AddMember(c.Request.Context(), orgID, memberID, body.Role)
	if err != nil {
		// An existing member keeps their role; changing it is
		// PATCH /orgs/:orgId/members/:userId, which guards the last owner.
		if errors.Is(err, store.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "user is already a member", "code": "already_member"})
			return
		}
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, member)
}

// PATCH /orgs/:orgId/members/:userId
func (h *OrgHandler) UpdateOrgMemberRole(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	if !h.orgOwnerGuard(c, orgID, user.ID) {
		return
	}
	memberID, ok := parseUUID(c, "userId")
	if !ok {
		return
	}
	var body struct {
		Role model.OrgRole `json:"role"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if !body.Role.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	// The store refuses to demote the last owner, checking and writing
	// under one lock so two owners can't both step down at once.
	member, err := h.Orgs.UpdateMemberRole(c.Request.Context(), orgID, memberID, body.Role)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		case errors.Is(err, store.ErrLastOwner):
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote the last owner"})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, member)
}

// DELETE /orgs/:orgId/members/:userId
func (h *OrgHandler) RemoveOrgMember(c *gin.Context) {
	user := auth.CurrentUser(c)
	if !requireSessionAuth(c) {
		return
	}
	orgID, ok := parseUUID(c, "orgId")
	if !ok {
		return
	}
	memberID, ok := parseUUID(c, "userId")
	if !ok {
		return
	}
	// Users can always remove themselves (self-leave).
	if memberID != user.ID {
		if !h.orgOwnerGuard(c, orgID, user.ID) {
			return
		}
	}
	if _, err := h.Orgs.GetMember(c.Request.Context(), orgID, memberID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}
	// The store refuses to remove the last owner (checked under a lock).
	if err := h.Orgs.RemoveMember(c.Request.Context(), orgID, memberID); err != nil {
		if errors.Is(err, store.ErrLastOwner) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot remove the last owner"})
			return
		}
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── Org helper guards ────────────────────────────────────────────────────────

// orgOwnerGuard returns true if userID is an owner; writes 403 or 404 otherwise.
func (h *OrgHandler) orgOwnerGuard(c *gin.Context, orgID, userID uuid.UUID) bool {
	m, err := h.Orgs.GetMember(c.Request.Context(), orgID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return false
	}
	if m.Role != model.OrgRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner role required"})
		return false
	}
	return true
}
