package main

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/handler"
)

// handlers holds all instantiated HTTP handlers for route registration.
type handlers struct {
	pages   *handler.PageHandler
	tasks   *handler.TaskHandler
	lanes   *handler.LaneHandler
	folders *handler.FolderHandler
	templates *handler.TemplateHandler
	orgs      *handler.OrgHandler
	shares    *handler.ShareHandler
}

// newHandlers creates all handler instances from their stores.
func newHandlers(stores *stores) *handlers {
	perms := &handler.PermissionChecker{Orgs: stores.orgs, Shares: stores.shares}

	return &handlers{
		pages:     &handler.PageHandler{Pages: stores.pages, Perms: perms},
		tasks:     &handler.TaskHandler{Tasks: stores.tasks, Perms: perms},
		lanes:     &handler.LaneHandler{Lanes: stores.lanes},
		folders: &handler.FolderHandler{
			Pages: stores.pages,
			Lanes: stores.lanes,
			Tasks: stores.tasks,
			Perms: perms,
		},
		templates: &handler.TemplateHandler{Templates: stores.templates},
		orgs:      &handler.OrgHandler{Orgs: stores.orgs, Users: stores.users},
		shares: &handler.ShareHandler{
			Shares:    stores.shares,
			Users:     stores.users,
			Orgs:      stores.orgs,
			Pages:     stores.pages,
			Tasks:     stores.tasks,
			Templates: stores.templates,
		},
	}
}

// registerRoutes adds all API v1 routes to the given router group.
func registerRoutes(apiGroup *gin.RouterGroup, h *handlers) {
	// Rate limiter for expensive endpoints (e.g., URL unfurling)
	unfurlLimiter := handler.NewRateLimiter(30, time.Minute)

	// Pages
	apiGroup.GET("/pages", h.pages.ListPages)
	apiGroup.POST("/pages", h.pages.CreatePage)
	apiGroup.GET("/pages/:id", h.pages.GetPage)
	apiGroup.PATCH("/pages/:id", h.pages.UpdatePage)
	apiGroup.PUT("/pages/:id", h.pages.UpsertPage)
	apiGroup.DELETE("/pages/:id", h.pages.DeletePage)
	apiGroup.GET("/pages/:id/content", h.pages.GetPageContent)
	apiGroup.PUT("/pages/:id/content", h.pages.UpsertPageContent)

	// Tasks
	apiGroup.GET("/tasks", h.tasks.ListTasks)
	apiGroup.POST("/tasks", h.tasks.CreateTask)
	apiGroup.POST("/tasks/filter", h.tasks.FilterTasks)
	apiGroup.GET("/tasks/:id", h.tasks.GetTask)
	apiGroup.PATCH("/tasks/:id", h.tasks.UpdateTask)
	apiGroup.PUT("/tasks/:id", h.tasks.UpsertTask)
	apiGroup.DELETE("/tasks/:id", h.tasks.DeleteTask)

	// Utilities
	apiGroup.POST("/unfurl", handler.RateLimitMiddleware(unfurlLimiter), handler.UnfurlURL)

	// Lanes
	apiGroup.GET("/lanes", h.lanes.ListLanes)
	apiGroup.POST("/lanes", h.lanes.CreateLane)
	apiGroup.POST("/lanes/batch", h.lanes.BatchCreateLanes)
	apiGroup.PUT("/lanes/reorder", h.lanes.ReorderLanes)
	apiGroup.GET("/lanes/:id", h.lanes.GetLane)
	apiGroup.PATCH("/lanes/:id", h.lanes.UpdateLane)
	apiGroup.PUT("/lanes/:id", h.lanes.UpsertLane)
	apiGroup.DELETE("/lanes/:id", h.lanes.DeleteLane)

	// Templates
	apiGroup.GET("/templates", h.templates.ListTemplates)
	apiGroup.POST("/templates", h.templates.CreateTemplate)
	apiGroup.GET("/templates/:id", h.templates.GetTemplate)
	apiGroup.PATCH("/templates/:id", h.templates.UpdateTemplate)
	apiGroup.PUT("/templates/:id", h.templates.UpsertTemplate)
	apiGroup.DELETE("/templates/:id", h.templates.DeleteTemplate)

	// Organizations
	apiGroup.GET("/orgs", h.orgs.ListOrgs)
	apiGroup.POST("/orgs", h.orgs.CreateOrg)
	apiGroup.GET("/orgs/:orgId", h.orgs.GetOrg)
	apiGroup.PATCH("/orgs/:orgId", h.orgs.UpdateOrg)
	apiGroup.DELETE("/orgs/:orgId", h.orgs.DeleteOrg)
	apiGroup.POST("/orgs/:orgId/members", h.orgs.AddOrgMember)
	apiGroup.PATCH("/orgs/:orgId/members/:userId", h.orgs.UpdateOrgMemberRole)
	apiGroup.DELETE("/orgs/:orgId/members/:userId", h.orgs.RemoveOrgMember)

	// Shares
	apiGroup.GET("/shares", h.shares.ListShares)
	apiGroup.POST("/shares", h.shares.CreateShare)
	apiGroup.PATCH("/shares/:shareId", h.shares.UpdateSharePermission)
	apiGroup.DELETE("/shares/:shareId", h.shares.DeleteShare)

	// User search
	apiGroup.GET("/users/search", h.shares.SearchUsers)

	// Folder boards
	apiGroup.GET("/folders/:id", h.folders.GetFolder)
	apiGroup.GET("/folders/:id/lanes", h.folders.ListFolderLanes)
	apiGroup.POST("/folders/:id/lanes", h.folders.CreateFolderLane)
	apiGroup.PUT("/folders/:id/lanes/:laneId", h.folders.UpdateFolderLane)
	apiGroup.DELETE("/folders/:id/lanes/:laneId", h.folders.DeleteFolderLane)
	apiGroup.GET("/folders/:id/tasks", h.folders.ListFolderTasks)
}
