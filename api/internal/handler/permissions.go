package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// PermissionChecker encapsulates the cross-cutting write-permission logic
// that multiple domain handlers share.
type PermissionChecker struct {
	Orgs   store.OrgStore
	Shares store.ShareStore
}

// CanWritePage checks if requesterID has write access to page p.
// Returns true and continues if allowed; returns false after writing 403 if not.
func (pc *PermissionChecker) CanWritePage(c *gin.Context, p *model.Page, requesterID uuid.UUID) bool {
	return pc.CanWriteResource(c, p.UserID, p.OrgID, model.ShareResourcePage, p.ID, requesterID)
}

// CanReadFolder checks if requesterID can read the folder with the given ID.
// Returns the folder on success; writes 404/403 and returns nil on failure.
func (pc *PermissionChecker) CanReadFolder(c *gin.Context, pages store.PageStore, folderID, requesterID uuid.UUID) *model.Page {
	ctx := c.Request.Context()
	folder, err := pages.GetFolderByID(ctx, folderID, requesterID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "folder not found"})
		return nil
	}
	if folder.Type != model.NodeTypeFolder {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not a folder"})
		return nil
	}
	// GetFolderByID already applies the three-tier access filter (including
	// resource_type = 'folder' share checks), so reaching here means the user
	// has read access.
	return folder
}

// CanWriteFolder checks if requesterID can write to the folder (create/edit lanes).
// It also accepts a pre-fetched folder to avoid a second DB round-trip.
func (pc *PermissionChecker) CanWriteFolder(c *gin.Context, folder *model.Page, requesterID uuid.UUID) bool {
	return pc.CanWriteResource(c, folder.UserID, folder.OrgID, model.ShareResourceFolder, folder.ID, requesterID)
}

// currentTokenScope returns the *model.TokenScope for the request, or nil if
// the request authenticated via session cookie (unrestricted).
func currentTokenScope(c *gin.Context) *model.TokenScope {
	v, ok := c.Get(model.TokenScopeContextKey)
	if !ok {
		return nil
	}
	ts, _ := v.(*model.TokenScope)
	return ts
}

// requireSessionAuth rejects a request authenticated via an OAuth bearer
// token, writing 403 and returning false; a cookie-session request always
// passes. Use this to close routes that have no corresponding OAuth scope at
// all (personal lanes, org administration, sharing, user directory search,
// folder boards) — rather than silently granting a bearer token the acting
// user's full permissions on them, which defeats the point of scoped
// delegation. Prefer checkTokenScope/CanReadResource/CanWriteResource instead
// whenever the resource has a real ShareResourceType scope to check against.
func requireSessionAuth(c *gin.Context) bool {
	if currentTokenScope(c) != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "this endpoint is not available to OAuth bearer tokens"})
		return false
	}
	return true
}

// requireOrgReadScope allows a cookie-session request through unconditionally
// and a bearer-token request only if it explicitly carries model.ScopeOrgRead.
// Organizations aren't scoped to another org, so the org-membership check
// scopeAllows performs doesn't apply here — this checks the scope list
// directly instead. Use for read-only org endpoints; org administration
// (create/update/delete/members) has no corresponding write scope at all and
// should use requireSessionAuth instead.
func requireOrgReadScope(c *gin.Context) bool {
	scope := currentTokenScope(c)
	if scope == nil {
		return true
	}
	if scope.HasScope(model.ScopeOrgRead) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "token scope does not permit this action"})
	return false
}

// scopeAllows is the pure, non-response-writing predicate behind
// checkTokenScope: the resource's org must be within the token's granted
// orgs (M2M/delegated tokens cannot reach a user's personal, non-org
// resources at all), and the token must carry the scope for this resource
// type + permission level. A nil scope (cookie-session request) always
// passes — it is unrestricted, governed only by the user's own permissions.
func scopeAllows(scope *model.TokenScope, orgID *uuid.UUID, resourceType model.ShareResourceType, write bool) bool {
	if scope == nil {
		return true
	}
	if orgID == nil || !scope.HasOrg(*orgID) {
		return false
	}
	for _, s := range scope.Scopes {
		rt, ok := s.ResourceType()
		if !ok || rt != resourceType {
			continue
		}
		if write && s.IsWrite() {
			return true
		}
		if !write {
			return true // read scope for this resource type present (read or write)
		}
	}
	return false
}

// checkTokenScope enforces scopeAllows for a single request, writing a 403
// JSON response and returning false when the token's grant doesn't cover
// the resource being accessed.
func checkTokenScope(c *gin.Context, orgID *uuid.UUID, resourceType model.ShareResourceType, write bool) bool {
	scope := currentTokenScope(c)
	if scopeAllows(scope, orgID, resourceType, write) {
		return true
	}
	if orgID == nil || !scope.HasOrg(*orgID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "resource outside token's org scope"})
		return false
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "token scope does not permit this action"})
	return false
}

// FilterByTokenScope drops any item whose org falls outside the requesting
// bearer token's granted scope (cookie-session requests are unrestricted
// and pass every item through unchanged). Used by List* handlers, since
// threading scope through every SQL query in the store layer would be far
// more invasive for the same result at Glyph's current scale.
func FilterByTokenScope[T any](c *gin.Context, resourceType model.ShareResourceType, items []T, orgIDOf func(T) *uuid.UUID) []T {
	scope := currentTokenScope(c)
	if scope == nil {
		return items
	}
	// A fresh backing array — items[:0] would instead overwrite the
	// caller's own backing array in place as we append, corrupting it for
	// any other reference the caller (or its caller) still holds.
	out := make([]T, 0, len(items))
	for _, it := range items {
		if scopeAllows(scope, orgIDOf(it), resourceType, false) {
			out = append(out, it)
		}
	}
	return out
}

// CanUseOrg verifies requesterID is a member of orgID before it is persisted
// onto a page/task/template — a nil orgID (personal resource) always
// passes. Without this, a create/upsert/update handler that merely copies a
// client-supplied orgId onto the row (relying on checkTokenScope, which only
// governs bearer tokens, not session requests) would let any user plant
// content inside an org they don't belong to; once shared non-privately,
// every member of that org can see — and, depending on their role, write
// to — a row owned by an outsider.
func (pc *PermissionChecker) CanUseOrg(c *gin.Context, orgID *uuid.UUID, requesterID uuid.UUID) bool {
	if orgID == nil || pc == nil || pc.Orgs == nil {
		return true
	}
	if _, err := pc.Orgs.GetMember(c.Request.Context(), *orgID, requesterID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusForbidden, gin.H{"error": "you are not a member of this organization"})
			return false
		}
		slog.Error("permission check failed (org membership)",
			"org_id", orgID,
			"requester_id", requesterID,
			"err", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return false
	}
	return true
}

// CanReadResource is the read-side twin of CanWriteResource's scope check —
// called after a store's own GetByID access filter already confirmed the
// requester can read the resource, to additionally enforce that a bearer
// token's scope covers it. Cookie-session requests (no TokenScope) always pass.
func (pc *PermissionChecker) CanReadResource(c *gin.Context, orgID *uuid.UUID, resourceType model.ShareResourceType) bool {
	return checkTokenScope(c, orgID, resourceType, false)
}

// CanWriteResource is the general write-permission check used by all resource types.
func (pc *PermissionChecker) CanWriteResource(
	c *gin.Context,
	ownerID uuid.UUID,
	orgID *uuid.UUID,
	resourceType model.ShareResourceType,
	resourceID uuid.UUID,
	requesterID uuid.UUID,
) bool {
	if !checkTokenScope(c, orgID, resourceType, true) {
		return false
	}
	if requesterID == ownerID {
		return true
	}
	ctx := c.Request.Context()
	// Check org role
	if orgID != nil && pc.Orgs != nil {
		m, err := pc.Orgs.GetMember(ctx, *orgID, requesterID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("permission check failed (org lookup)",
					"org_id", orgID,
					"requester_id", requesterID,
					"err", err,
				)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
				return false
			}
			// Not found means user is not a member — continue to next check
		} else if m.Role == model.OrgRoleOwner || m.Role == model.OrgRoleEditor {
			return true
		}
	}
	// Check direct share
	if pc.Shares != nil {
		s, err := pc.Shares.GetForUserAndResource(ctx, requesterID, resourceType, resourceID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("permission check failed (share lookup)",
					"resource_id", resourceID,
					"requester_id", requesterID,
					"err", err,
				)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
				return false
			}
			// Not found means no share — fall through to denied
		} else if s.Permission == model.SharePermissionEditor {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "write access denied"})
	return false
}
