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

// CanWriteResource is the general write-permission check used by all resource types.
func (pc *PermissionChecker) CanWriteResource(
	c *gin.Context,
	ownerID uuid.UUID,
	orgID *uuid.UUID,
	resourceType model.ShareResourceType,
	resourceID uuid.UUID,
	requesterID uuid.UUID,
) bool {
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
