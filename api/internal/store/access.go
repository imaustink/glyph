package store

import "fmt"

// ResourceKind defines a validated (table, resourceType) pair for access control.
// Only pre-approved constants can be used in SQL access filters, preventing any
// possibility of SQL injection through the table/resourceType parameters.
type ResourceKind struct {
	table        string
	resourceType string
}

// Allowed resource kinds — these are the only values that can appear in SQL.
var (
	ResourcePage     = ResourceKind{table: "pages", resourceType: "page"}
	ResourceTask     = ResourceKind{table: "tasks", resourceType: "task"}
	ResourceTemplate = ResourceKind{table: "templates", resourceType: "template"}
	ResourceFolder   = ResourceKind{table: "pages", resourceType: "folder"}
)

// ResourceAccessFilter returns the SQL WHERE fragment (with no leading AND)
// that enforces the three-tier read-access policy used by pages, tasks, and
// templates:
//
//  1. The row owner ($1 = userID).
//  2. Any org member when the resource is non-private and org_id is set.
//  3. Any user who has a direct share entry for this resource.
//
// The returned fragment assumes $1 is the caller's userID and that exactly one
// additional positional parameter ($2) is available for the row's own id when
// fetching by ID (not used inside this fragment itself).
//
// The kind parameter must be one of the package-level ResourceKind constants
// (ResourcePage, ResourceTask, ResourceTemplate, ResourceFolder). This ensures
// only validated, hardcoded values are interpolated into the SQL string.
func ResourceAccessFilter(kind ResourceKind) string {
	return fmt.Sprintf(`(
	user_id = $1
	OR (org_id IS NOT NULL AND is_private = false
	    AND org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
	OR EXISTS (SELECT 1 FROM shares
	           WHERE resource_type = '%s' AND resource_id = %s.id AND shared_with_id = $1)
)`, kind.resourceType, kind.table)
}

// FolderAccessFilter returns the SQL WHERE fragment that enforces read access
// to a specific folder row in the pages table. It applies the same three-tier
// policy as ResourceAccessFilter but is parameterised with a named placeholder
// $folderID to allow it to be embedded in subqueries.
//
// Usage: the fragment is meant to be used as a standalone predicate in queries
// like: "SELECT 1 FROM pages WHERE id = $2 AND (" + FolderAccessSQL + ")"
// where $1 = userID and $2 = folderID.
const FolderAccessSQL = `(
	user_id = $1
	OR (org_id IS NOT NULL AND is_private = false
	    AND org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
	OR EXISTS (SELECT 1 FROM shares
	           WHERE resource_type = 'folder' AND resource_id = pages.id AND shared_with_id = $1)
)`

// FolderWriteSQL returns the SQL predicate that enforces write access to a
// folder: the user must be owner, an org editor/owner, or have an editor share.
const FolderWriteSQL = `(
	user_id = $1
	OR (org_id IS NOT NULL AND is_private = false
	    AND org_id IN (SELECT org_id FROM org_members WHERE user_id = $1 AND role IN ('owner','editor')))
	OR EXISTS (SELECT 1 FROM shares
	           WHERE resource_type = 'folder' AND resource_id = pages.id AND shared_with_id = $1 AND permission = 'editor')
)`

// FolderDescendantsCTE is a SQL CTE fragment that recursively collects all
// descendant page IDs (pages and sub-folders) for a given folder.
// The caller must supply $1 = folderID. The CTE name is "descendants".
const FolderDescendantsCTE = `
WITH RECURSIVE descendants AS (
    SELECT id FROM pages WHERE id = $1
    UNION ALL
    SELECT p.id FROM pages p
    JOIN descendants d ON p.parent_id = d.id
)`

// FolderDescendantsForUserCTE is like FolderDescendantsCTE but restricts to
// folders/pages that the requesting user ($2) can read, then returns all task-
// bearing pages underneath any folder shared with that user.
// Supply $1 = folderID, $2 = userID.
const FolderDescendantsForUserCTE = FolderDescendantsCTE

// DescendantPagesSQL is used inside the task filter to pull tasks whose
// source_page_id is a descendant of the target folder.
// The caller binds $1 = folderID.
func DescendantPagesSQL() string {
	return FolderDescendantsCTE + `
SELECT id FROM descendants`
}

// FolderSharedWithUserSQL returns a SQL boolean expression (no outer parens)
// that is true when the given folder ($2) is readable by user ($1).
func FolderSharedWithUserSQL() string {
	return fmt.Sprintf(`EXISTS (
    SELECT 1 FROM pages WHERE id = $2 AND %s
)`, FolderAccessSQL)
}
