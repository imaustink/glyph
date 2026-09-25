package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Enums ────────────────────────────────────────────────────────────────────

type Priority string

const (
	PriorityUrgent Priority = "urgent"
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
	PriorityNone   Priority = "none"
)

// IsValid returns true if p is a known priority value.
func (p Priority) IsValid() bool {
	switch p {
	case PriorityUrgent, PriorityHigh, PriorityMedium, PriorityLow, PriorityNone:
		return true
	}
	return false
}

type TaskStatus string

const (
	StatusTodo       TaskStatus = "todo"
	StatusInProgress TaskStatus = "in-progress"
	StatusDone       TaskStatus = "done"
	StatusCancelled  TaskStatus = "cancelled"
)

// IsValid returns true if s is a known task status value.
func (s TaskStatus) IsValid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusDone, StatusCancelled:
		return true
	}
	return false
}

type TreeNodeType string

const (
	NodeTypePage   TreeNodeType = "page"
	NodeTypeFolder TreeNodeType = "folder"
)

type SortMode string

const (
	SortModeAuto   SortMode = "auto"
	SortModeField  SortMode = "field"
	SortModeManual SortMode = "manual"
)

type SortDirection string

const (
	SortDirectionAsc  SortDirection = "asc"
	SortDirectionDesc SortDirection = "desc"
)

type FilterConjunction string

const (
	ConjunctionAnd FilterConjunction = "and"
	ConjunctionOr  FilterConjunction = "or"
)

type FilterOperator string

const (
	FilterOpEq        FilterOperator = "eq"
	FilterOpNeq       FilterOperator = "neq"
	FilterOpIn        FilterOperator = "in"
	FilterOpNotIn     FilterOperator = "not_in"
	FilterOpContains  FilterOperator = "contains"
	FilterOpBefore    FilterOperator = "before"
	FilterOpAfter     FilterOperator = "after"
	FilterOpAny       FilterOperator = "any"
	FilterOpExists    FilterOperator = "exists"
	FilterOpNotExists FilterOperator = "not_exists"
)

// ─── User ─────────────────────────────────────────────────────────────────────

// User is created (or fetched) on first OIDC login.
type User struct {
	ID        uuid.UUID `json:"id"`
	Sub       string    `json:"sub"`
	Issuer    string    `json:"issuer"`
	Email     *string   `json:"email,omitempty"`
	Name      *string   `json:"name,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ─── Pages ────────────────────────────────────────────────────────────────────

type TodoTriggerMatchMode string

const (
	MatchModeExact TodoTriggerMatchMode = "exact"
	MatchModeRegex TodoTriggerMatchMode = "regex"
)

type TodoTriggerConfig struct {
	Pattern    string               `json:"pattern"`
	MatchMode  TodoTriggerMatchMode `json:"matchMode"`
	BlockTypes []string             `json:"blockTypes"`
}

type Page struct {
	ID          uuid.UUID          `json:"id"`
	UserID      uuid.UUID          `json:"userId"`
	Type        TreeNodeType       `json:"type"`
	Title       string             `json:"title" binding:"max=500"`
	ParentID    *uuid.UUID         `json:"parentId"`
	Order       int                `json:"order" binding:"gte=0"`
	Tags        []string           `json:"tags"`
	Priority    Priority           `json:"priority" binding:"omitempty,priority"`
	TodoTrigger *TodoTriggerConfig `json:"todoTrigger,omitempty"`
	OrgID       *uuid.UUID         `json:"orgId,omitempty"`
	IsPrivate   bool               `json:"isPrivate"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type PageContent struct {
	PageID    uuid.UUID       `json:"pageId"`
	Content   json.RawMessage `json:"content"` // ProseMirror JSON document stored as JSONB
	UpdatedAt time.Time       `json:"updatedAt"`
	// SchemaVersion tracks the ProseMirror document schema so that
	// content can be migrated when extensions change. 0/absent = pre-versioning
	// era (treated as v1 by clients).
	SchemaVersion int `json:"schemaVersion"`
	// Revision increments on every successful content write. Clients echo the
	// revision they last read back as ExpectedRevision so a write derived from
	// a stale read is rejected rather than silently overwriting newer content.
	Revision int `json:"revision"`
	// ExpectedRevision is request-only: the revision the client believes it is
	// updating. Required whenever the page already has content — a write
	// without it is rejected as a conflict. Never serialised in responses.
	ExpectedRevision int `json:"expectedRevision,omitempty"`
	// DetachCollab is server-side only (never bound from JSON). When true, a
	// write to a page attached to a collaborative session detaches the page
	// (making page_contents authoritative again) instead of being refused. Set
	// only when collaborative editing is disabled server-wide.
	DetachCollab bool `json:"-"`
}

// CollabSnapshot is the collab service writing the current state of a shared
// document back to page_contents.
type CollabSnapshot struct {
	PageID uuid.UUID `json:"-"`
	// Epoch the snapshot was produced in. Rejected unless it is current.
	Epoch int `json:"epoch" binding:"required,gte=1"`
	// UpToSeq is the highest update-log seq reflected in Content. Rejected if
	// lower than the last accepted snapshot, so a lagging replica can't move
	// content backwards.
	UpToSeq       int64           `json:"upToSeq" binding:"gte=0"`
	Content       json.RawMessage `json:"content" binding:"required"`
	SchemaVersion int             `json:"schemaVersion"`
}

// CollabSession is what the collab service needs to admit a WebSocket
// connection for a page: who the user is and what they may do.
type CollabSession struct {
	Enabled  bool      `json:"enabled"`
	PageID   uuid.UUID `json:"pageId"`
	UserID   uuid.UUID `json:"userId"`
	Name     string    `json:"name"`
	CanWrite bool      `json:"canWrite"`
}

// PageContentVersion is a superseded revision of a page's content, retained so
// an unintended overwrite can be recovered in-product rather than via a
// database point-in-time restore.
type PageContentVersion struct {
	ID            int64           `json:"id"`
	PageID        uuid.UUID       `json:"pageId"`
	Content       json.RawMessage `json:"content"`
	Revision      int             `json:"revision"`
	SchemaVersion int             `json:"schemaVersion"`
	ReplacedAt    time.Time       `json:"replacedAt"`
}

// ─── Tasks ────────────────────────────────────────────────────────────────────

type LinkMeta struct {
	URL         string  `json:"url"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Image       *string `json:"image"`
	Favicon     *string `json:"favicon"`
	SiteName    *string `json:"siteName"`
}

type Task struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"userId"`
	Title        string     `json:"title" binding:"max=500"`
	Description  string     `json:"description" binding:"max=10000"`
	Status       TaskStatus `json:"status" binding:"omitempty,taskstatus"`
	Priority     Priority   `json:"priority" binding:"omitempty,priority"`
	Tags         []string   `json:"tags"`
	DueDate      *string    `json:"dueDate"` // ISO date string YYYY-MM-DD
	SourcePageID *uuid.UUID `json:"sourcePageId"`
	SourceNodeID *string    `json:"sourceNodeId"`
	Link         *LinkMeta  `json:"link"`
	Order        int        `json:"order" binding:"gte=0"`
	OrgID        *uuid.UUID `json:"orgId,omitempty"`
	IsPrivate    bool       `json:"isPrivate"`
	// FolderID scopes a standalone task (sourcePageId = nil) to a folder board.
	FolderID  *uuid.UUID `json:"folderId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// ─── Lanes ────────────────────────────────────────────────────────────────────

type FilterRule struct {
	ID       string         `json:"id"`
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Value    interface{}    `json:"value"`
}

type FilterSet struct {
	Conjunction FilterConjunction `json:"conjunction"`
	Rules       []FilterRule      `json:"rules"`
}

type SortConfig struct {
	Mode      SortMode       `json:"mode"`
	Field     *string        `json:"field,omitempty"`
	Direction *SortDirection `json:"direction,omitempty"`
	TaskOrder []string       `json:"taskOrder,omitempty"`
}

type Lane struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"userId"`
	Title      string     `json:"title" binding:"required,min=1,max=100"`
	FilterSet  FilterSet  `json:"filterSet"`
	SortConfig SortConfig `json:"sortConfig"`
	Order      int        `json:"order" binding:"gte=0"`
	// FolderID scopes this lane to a folder board. NULL = personal board lane.
	FolderID  *uuid.UUID `json:"folderId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// ─── Templates ────────────────────────────────────────────────────────────────

type Template struct {
	ID              uuid.UUID          `json:"id"`
	UserID          uuid.UUID          `json:"userId"`
	Name            string             `json:"name"`
	Content         string             `json:"content"`
	TitleTemplate   string             `json:"titleTemplate"`
	TodoTrigger     *TodoTriggerConfig `json:"todoTrigger,omitempty"`
	DefaultFolderID *uuid.UUID         `json:"defaultFolderId"`
	IsDefault       bool               `json:"isDefault"`
	OrgID           *uuid.UUID         `json:"orgId,omitempty"`
	IsPrivate       bool               `json:"isPrivate"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

// ─── Organizations ────────────────────────────────────────────────────────────

type OrgRole string

const (
	OrgRoleOwner  OrgRole = "owner"
	OrgRoleEditor OrgRole = "editor"
	OrgRoleViewer OrgRole = "viewer"
)

// IsValid returns true if r is a known org role value. An unrecognized role
// silently confers no privileges anywhere it's checked (every role
// comparison in this codebase is `== OrgRoleOwner` / `== OrgRoleEditor`, an
// unknown value never matches either), but callers should still validate
// with this before persisting one — the DB CHECK constraint only turns the
// same problem into a 500 instead of a 400.
func (r OrgRole) IsValid() bool {
	switch r {
	case OrgRoleOwner, OrgRoleEditor, OrgRoleViewer:
		return true
	}
	return false
}

type Organization struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	CreatedBy   uuid.UUID `json:"createdBy"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// OrgWithRole is Organization plus the requesting user's role — returned from
// list/get endpoints so the frontend knows what actions are permitted.
type OrgWithRole struct {
	Organization
	Role OrgRole `json:"role"`
}

type OrgMember struct {
	OrgID    uuid.UUID `json:"orgId"`
	UserID   uuid.UUID `json:"userId"`
	Email    *string   `json:"email,omitempty"`
	Name     *string   `json:"name,omitempty"`
	Role     OrgRole   `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// ─── Shares ───────────────────────────────────────────────────────────────────

type SharePermission string

const (
	SharePermissionViewer SharePermission = "viewer"
	SharePermissionEditor SharePermission = "editor"
)

// IsValid returns true if p is a known share permission value.
func (p SharePermission) IsValid() bool {
	switch p {
	case SharePermissionViewer, SharePermissionEditor:
		return true
	}
	return false
}

type ShareResourceType string

const (
	ShareResourcePage     ShareResourceType = "page"
	ShareResourceTask     ShareResourceType = "task"
	ShareResourceTemplate ShareResourceType = "template"
	ShareResourceFolder   ShareResourceType = "folder"
)

// IsValid returns true if t is a known share resource type value.
func (t ShareResourceType) IsValid() bool {
	switch t {
	case ShareResourcePage, ShareResourceTask, ShareResourceTemplate, ShareResourceFolder:
		return true
	}
	return false
}

type Share struct {
	ID           uuid.UUID         `json:"id"`
	ResourceType ShareResourceType `json:"resourceType"`
	ResourceID   uuid.UUID         `json:"resourceId"`
	SharedByID   uuid.UUID         `json:"sharedById"`
	SharedWith   ShareUser         `json:"sharedWith"`
	Permission   SharePermission   `json:"permission"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// ShareUser is a minimal user projection embedded in Share responses.
type ShareUser struct {
	ID    uuid.UUID `json:"id"`
	Email *string   `json:"email,omitempty"`
	Name  *string   `json:"name,omitempty"`
}

// UserSearchResult is returned by the user search endpoint.
type UserSearchResult struct {
	ID    uuid.UUID `json:"id"`
	Email *string   `json:"email,omitempty"`
	Name  *string   `json:"name,omitempty"`
}
