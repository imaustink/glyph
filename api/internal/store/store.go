package store

import (
	"context"
	"encoding/json"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBPool is the minimal database access interface used by all Postgres store
// implementations. *pgxpool.Pool satisfies this interface.
type DBPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// jsonMarshal is package-level so tests can override it to inject marshal errors.
var jsonMarshal = json.Marshal

// UserStore handles persistence for users (upserted on OIDC login).
type UserStore interface {
	Upsert(ctx context.Context, sub, issuer string, email, name *string) (*model.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	// Search returns users whose email or name match the query (case-insensitive
	// substring match). excludeID is excluded from results (typically the caller).
	// orgIDs restricts results to members of those orgs (pass nil to search all).
	Search(ctx context.Context, query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error)
}

// PageStore handles the page/folder tree.
type PageStore interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Page, error)
	ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg Pagination) ([]*model.Page, int, error)
	GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Page, error)
	// GetFolderByID is like GetByID but checks folder-type shares
	// (resource_type = 'folder') instead of page shares, so that callers
	// who shared a folder receive the correct access-granted result.
	GetFolderByID(ctx context.Context, id, userID uuid.UUID) (*model.Page, error)
	Create(ctx context.Context, p *model.Page) (*model.Page, error)
	Update(ctx context.Context, p *model.Page) (*model.Page, error)
	Upsert(ctx context.Context, p *model.Page) (*model.Page, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error

	GetContent(ctx context.Context, pageID, userID uuid.UUID) (*model.PageContent, error)
	UpsertContent(ctx context.Context, pc *model.PageContent, userID uuid.UUID) (*model.PageContent, error)

	// IsAncestor reports whether candidateAncestorID is an ancestor of nodeID in
	// the page tree. Returns false when either ID does not exist. Used to prevent
	// reparenting operations that would create a cycle.
	IsAncestor(ctx context.Context, candidateAncestorID, nodeID uuid.UUID) (bool, error)

	// GetDescendantIDs returns the IDs of all descendant pages/folders rooted at
	// folderID (inclusive). Used by folder board queries to scope tasks/lanes.
	GetDescendantIDs(ctx context.Context, folderID uuid.UUID) ([]uuid.UUID, error)
}

// TaskStore handles task persistence.
type TaskStore interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Task, error)
	ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg Pagination) ([]*model.Task, int, error)
	ListBySourcePage(ctx context.Context, userID uuid.UUID, pageID uuid.UUID) ([]*model.Task, error)
	ListBySourceNode(ctx context.Context, userID uuid.UUID, sourceNodeID string) ([]*model.Task, error)
	// ListByFilter returns tasks matching the given FilterSet for the user.
	ListByFilter(ctx context.Context, userID uuid.UUID, fs model.FilterSet) ([]*model.Task, error)
	// ListByFolder returns all tasks for a folder board: tasks whose source_page_id
	// is a descendant of folderID, plus tasks with tasks.folder_id = folderID.
	// descendantPageIDs must be pre-computed by PageStore.GetDescendantIDs.
	ListByFolder(ctx context.Context, folderID uuid.UUID, descendantPageIDs []uuid.UUID) ([]*model.Task, error)
	GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Task, error)
	Create(ctx context.Context, t *model.Task) (*model.Task, error)
	Update(ctx context.Context, t *model.Task) (*model.Task, error)
	Upsert(ctx context.Context, t *model.Task) (*model.Task, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
}

// LaneReorderItem is used by ReorderAll to batch-update lane ordering.
type LaneReorderItem struct {
	ID    uuid.UUID `json:"id"`
	Order int       `json:"order"`
}

// LaneStore handles kanban lane persistence.
type LaneStore interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Lane, error)
	// ListByFolder returns lanes scoped to the given folder, accessible by userID.
	ListByFolder(ctx context.Context, folderID, userID uuid.UUID) ([]*model.Lane, error)
	GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Lane, error)
	// GetByIDAndFolder fetches a lane by ID, verifying it belongs to the given folder.
	GetByIDAndFolder(ctx context.Context, id, folderID uuid.UUID) (*model.Lane, error)
	Create(ctx context.Context, l *model.Lane) (*model.Lane, error)
	BatchCreate(ctx context.Context, lanes []*model.Lane) ([]*model.Lane, error)
	Update(ctx context.Context, l *model.Lane) (*model.Lane, error)
	// UpdateByIDAndFolder updates a folder-scoped lane regardless of who created it.
	UpdateByIDAndFolder(ctx context.Context, l *model.Lane, folderID uuid.UUID) (*model.Lane, error)
	Upsert(ctx context.Context, l *model.Lane) (*model.Lane, error)
	ReorderAll(ctx context.Context, userID uuid.UUID, items []LaneReorderItem) error
	Delete(ctx context.Context, id, userID uuid.UUID) error
	// DeleteByIDAndFolder deletes a lane by ID if it belongs to the given folder.
	DeleteByIDAndFolder(ctx context.Context, id, folderID uuid.UUID) error
}

// TemplateStore handles note template persistence.
type TemplateStore interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Template, error)
	GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Template, error)
	Create(ctx context.Context, t *model.Template) (*model.Template, error)
	Update(ctx context.Context, t *model.Template) (*model.Template, error)
	Upsert(ctx context.Context, t *model.Template) (*model.Template, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
}

// OrgStore handles organization and membership persistence.
type OrgStore interface {
	Create(ctx context.Context, org *model.Organization) (*model.Organization, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.Organization, error)
	// ListForUser returns all orgs the user belongs to, with their role.
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*model.OrgWithRole, error)
	Update(ctx context.Context, org *model.Organization) (*model.Organization, error)
	Delete(ctx context.Context, id uuid.UUID) error

	AddMember(ctx context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error)
	GetMember(ctx context.Context, orgID, userID uuid.UUID) (*model.OrgMember, error)
	ListMembers(ctx context.Context, orgID uuid.UUID) ([]*model.OrgMember, error)
	UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error)
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error
	// GetUserOrgIDs returns all org IDs the user belongs to (for access checks).
	GetUserOrgIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

// ShareStore handles per-item direct share persistence.
type ShareStore interface {
	Create(ctx context.Context, s *model.Share) (*model.Share, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.Share, error)
	// ListForResource returns all shares for a given resource.
	ListForResource(ctx context.Context, resourceType model.ShareResourceType, resourceID uuid.UUID) ([]*model.Share, error)
	// GetForUserAndResource returns the share for a specific user+resource, if one exists.
	GetForUserAndResource(ctx context.Context, userID uuid.UUID, resourceType model.ShareResourceType, resourceID uuid.UUID) (*model.Share, error)
	UpdatePermission(ctx context.Context, id uuid.UUID, permission model.SharePermission) (*model.Share, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
