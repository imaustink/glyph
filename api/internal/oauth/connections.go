package oauth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

type connectionOrg struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type connection struct {
	ID         uuid.UUID          `json:"id"`
	ClientName string             `json:"clientName"`
	Dynamic    bool               `json:"dynamic"`
	Scopes     []model.OAuthScope `json:"scopes"`
	Personal   bool               `json:"personal"`
	Orgs       []connectionOrg    `json:"orgs"`
	CreatedAt  time.Time          `json:"createdAt"`
	LastUsedAt *time.Time         `json:"lastUsedAt"`
}

// ListConnectionsHandler handles GET /api/v1/oauth/connections: the apps the
// current user has approved on the consent screen and that still hold a live
// grant, for the "Connected apps" settings page.
//
// Only authorization_code grants are listed. client_credentials tokens are
// minted by an org's own pre-registered client without the user's
// involvement; they're managed (and revoked) by org owners from the org's
// OAuth client settings instead.
func ListConnectionsHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireInteractiveUser(c) {
			return
		}
		user := auth.CurrentUser(c)
		ctx := c.Request.Context()
		tokens, err := cfg.Tokens.ListActiveForUser(ctx, user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}

		clients := map[uuid.UUID]*model.OAuthClient{}
		orgNames := map[uuid.UUID]string{}
		out := make([]connection, 0, len(tokens))
		for _, t := range tokens {
			if t.GrantType != model.GrantAuthorizationCode {
				continue
			}
			client, ok := clients[t.ClientID]
			if !ok {
				client, err = cfg.Clients.GetByID(ctx, t.ClientID)
				if err != nil {
					continue
				}
				clients[t.ClientID] = client
			}
			if client.RevokedAt != nil {
				continue
			}
			orgs := make([]connectionOrg, 0, len(t.OrgIDs))
			for _, orgID := range t.OrgIDs {
				name, ok := orgNames[orgID]
				if !ok {
					org, err := cfg.Orgs.GetByID(ctx, orgID)
					if err != nil {
						continue // org deleted since the grant; nothing left to reach
					}
					name = org.Name
					orgNames[orgID] = name
				}
				orgs = append(orgs, connectionOrg{ID: orgID, Name: name})
			}
			out = append(out, connection{
				ID:         t.ID,
				ClientName: client.Name,
				Dynamic:    client.IsDynamic,
				Scopes:     t.Scopes,
				Personal:   t.IncludePersonal,
				Orgs:       orgs,
				CreatedAt:  t.CreatedAt,
				LastUsedAt: t.LastUsedAt,
			})
		}
		c.JSON(http.StatusOK, out)
	}
}

// RevokeConnectionHandler handles DELETE /api/v1/oauth/connections/:id,
// revoking one of the current user's own grants (access and refresh token).
func RevokeConnectionHandler(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireInteractiveUser(c) {
			return
		}
		user := auth.CurrentUser(c)
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}
		ctx := c.Request.Context()
		tokens, err := cfg.Tokens.ListActiveForUser(ctx, user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		for _, t := range tokens {
			// Scoped to the caller's own live grants, so one user can never
			// revoke (or probe for) another user's token id.
			if t.ID == id && t.GrantType == model.GrantAuthorizationCode {
				if err := cfg.Tokens.Revoke(ctx, id); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
					return
				}
				c.Status(http.StatusNoContent)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	}
}
