package store

// Pagination holds query parameters for paginated list endpoints.
type Pagination struct {
	Limit  int
	Offset int
}

// PaginatedResult wraps a total count alongside the items for a paginated response.
type PaginatedResult[T any] struct {
	Items []*T
	Total int
}

// DefaultPagination returns a Pagination with sensible defaults.
// limit=0 means "use default" (500). Maximum enforced limit is 1000.
func DefaultPagination(limit, offset int) Pagination {
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	return Pagination{Limit: limit, Offset: offset}
}
