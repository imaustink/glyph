package handler

import (
	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

// ─── Update DTOs ──────────────────────────────────────────────────────────────
// These restrict the set of fields a client can modify via PATCH requests.
// Fields like ID, UserID, and CreatedAt are never accepted from the client.

// UpdatePageRequest defines the allowlisted fields for PATCH /pages/:id.
type UpdatePageRequest struct {
	Type        *model.TreeNodeType      `json:"type"`
	Title       *string                  `json:"title" binding:"omitempty,max=500"`
	ParentID    *uuid.UUID               `json:"parentId"`
	Order       *int                     `json:"order" binding:"omitempty,gte=0"`
	Tags        *[]string                `json:"tags"`
	TodoTrigger *model.TodoTriggerConfig `json:"todoTrigger"`
	OrgID       *uuid.UUID               `json:"orgId"`
	IsPrivate   *bool                    `json:"isPrivate"`
}

// ApplyTo merges non-nil fields from the request into the existing page.
func (r *UpdatePageRequest) ApplyTo(p *model.Page) {
	if r.Type != nil {
		p.Type = *r.Type
	}
	if r.Title != nil {
		p.Title = *r.Title
	}
	if r.ParentID != nil {
		p.ParentID = r.ParentID
	}
	if r.Order != nil {
		p.Order = *r.Order
	}
	if r.Tags != nil {
		p.Tags = *r.Tags
	}
	if r.TodoTrigger != nil {
		p.TodoTrigger = r.TodoTrigger
	}
	if r.OrgID != nil {
		p.OrgID = r.OrgID
	}
	if r.IsPrivate != nil {
		p.IsPrivate = *r.IsPrivate
	}
}

// UpdateTaskRequest defines the allowlisted fields for PATCH /tasks/:id.
type UpdateTaskRequest struct {
	Title        *string           `json:"title" binding:"omitempty,max=500"`
	Description  *string           `json:"description" binding:"omitempty,max=10000"`
	Status       *model.TaskStatus `json:"status" binding:"omitempty,taskstatus"`
	Priority     *model.Priority   `json:"priority" binding:"omitempty,priority"`
	Tags         *[]string         `json:"tags"`
	DueDate      *string           `json:"dueDate"`
	SourcePageID *uuid.UUID        `json:"sourcePageId"`
	SourceNodeID *string           `json:"sourceNodeId"`
	Link         *model.LinkMeta   `json:"link"`
	Order        *int              `json:"order" binding:"omitempty,gte=0"`
	OrgID        *uuid.UUID        `json:"orgId"`
	IsPrivate    *bool             `json:"isPrivate"`
}

// ApplyTo merges non-nil fields from the request into the existing task.
func (r *UpdateTaskRequest) ApplyTo(t *model.Task) {
	if r.Title != nil {
		t.Title = *r.Title
	}
	if r.Description != nil {
		t.Description = *r.Description
	}
	if r.Status != nil {
		t.Status = *r.Status
	}
	if r.Priority != nil {
		t.Priority = *r.Priority
	}
	if r.Tags != nil {
		t.Tags = *r.Tags
	}
	if r.DueDate != nil {
		t.DueDate = r.DueDate
	}
	if r.SourcePageID != nil {
		t.SourcePageID = r.SourcePageID
	}
	if r.SourceNodeID != nil {
		t.SourceNodeID = r.SourceNodeID
	}
	if r.Link != nil {
		t.Link = r.Link
	}
	if r.Order != nil {
		t.Order = *r.Order
	}
	if r.OrgID != nil {
		t.OrgID = r.OrgID
	}
	if r.IsPrivate != nil {
		t.IsPrivate = *r.IsPrivate
	}
}

// UpdateLaneRequest defines the allowlisted fields for PATCH /lanes/:id.
type UpdateLaneRequest struct {
	Title      *string           `json:"title" binding:"omitempty,min=1,max=100"`
	FilterSet  *model.FilterSet  `json:"filterSet"`
	SortConfig *model.SortConfig `json:"sortConfig"`
	Order      *int              `json:"order" binding:"omitempty,gte=0"`
}

// ApplyTo merges non-nil fields from the request into the existing lane.
func (r *UpdateLaneRequest) ApplyTo(l *model.Lane) {
	if r.Title != nil {
		l.Title = *r.Title
	}
	if r.FilterSet != nil {
		l.FilterSet = *r.FilterSet
	}
	if r.SortConfig != nil {
		l.SortConfig = *r.SortConfig
	}
	if r.Order != nil {
		l.Order = *r.Order
	}
}

// UpdateTemplateRequest defines the allowlisted fields for PATCH /templates/:id.
type UpdateTemplateRequest struct {
	Name            *string                  `json:"name"`
	Content         *string                  `json:"content"`
	TitleTemplate   *string                  `json:"titleTemplate"`
	TodoTrigger     *model.TodoTriggerConfig `json:"todoTrigger"`
	DefaultFolderID *uuid.UUID               `json:"defaultFolderId"`
	IsDefault       *bool                    `json:"isDefault"`
	OrgID           *uuid.UUID               `json:"orgId"`
	IsPrivate       *bool                    `json:"isPrivate"`
}

// ApplyTo merges non-nil fields from the request into the existing template.
func (r *UpdateTemplateRequest) ApplyTo(t *model.Template) {
	if r.Name != nil {
		t.Name = *r.Name
	}
	if r.Content != nil {
		t.Content = *r.Content
	}
	if r.TitleTemplate != nil {
		t.TitleTemplate = *r.TitleTemplate
	}
	if r.TodoTrigger != nil {
		t.TodoTrigger = r.TodoTrigger
	}
	if r.DefaultFolderID != nil {
		t.DefaultFolderID = r.DefaultFolderID
	}
	if r.IsDefault != nil {
		t.IsDefault = *r.IsDefault
	}
	if r.OrgID != nil {
		t.OrgID = r.OrgID
	}
	if r.IsPrivate != nil {
		t.IsPrivate = *r.IsPrivate
	}
}
