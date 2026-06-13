package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
)

// ─── Upsert ──────────────────────────────────────────────────────────────────

func TestUserStore_Upsert_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewUserStore(pool)
	email := "test@example.com"
	name := "Test"
	_, err := s.Upsert(context.Background(), "sub", "issuer", &email, &name)
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetByID ─────────────────────────────────────────────────────────────────

func TestUserStore_GetByID_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewUserStore(pool)
	_, err := s.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestUserStore_GetByID_NotFound(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}
	s := NewUserStore(pool)
	_, err := s.GetByID(context.Background(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── GetByEmail ──────────────────────────────────────────────────────────────

func TestUserStore_GetByEmail_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewUserStore(pool)
	_, err := s.GetByEmail(context.Background(), "test@example.com")
	if err == nil {
		t.Error("expected error")
	}
}

func TestUserStore_GetByEmail_NotFound(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}
	s := NewUserStore(pool)
	_, err := s.GetByEmail(context.Background(), "nope@example.com")
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Search ──────────────────────────────────────────────────────────────────

func TestUserStore_Search_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewUserStore(pool)
	_, err := s.Search(context.Background(), "query", uuid.New(), nil, 10)
	if err == nil {
		t.Error("expected error")
	}
}

func TestUserStore_Search_WithOrgIDs_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewUserStore(pool)
	_, err := s.Search(context.Background(), "query", uuid.New(), []uuid.UUID{uuid.New()}, 10)
	if err == nil {
		t.Error("expected error")
	}
}

func TestUserStore_Search_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewUserStore(pool)
	_, err := s.Search(context.Background(), "query", uuid.New(), nil, 10)
	if err == nil {
		t.Error("expected error")
	}
}

func TestUserStore_Search_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewUserStore(pool)
	_, err := s.Search(context.Background(), "query", uuid.New(), nil, 10)
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Success paths ────────────────────────────────────────────────────────────

func makeUserRowFn(id uuid.UUID) func(dest ...any) error {
return func(dest ...any) error {
*dest[0].(*uuid.UUID) = id
*dest[1].(*string) = "sub"
*dest[2].(*string) = "issuer"
*dest[3].(**string) = nil // email
*dest[4].(**string) = nil // name
*dest[5].(*time.Time) = time.Time{}
*dest[6].(*time.Time) = time.Time{}
return nil
}
}

func TestUserStore_Upsert_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeUserRowFn(id)}
},
}
s := NewUserStore(pool)
got, err := s.Upsert(context.Background(), "sub", "issuer", nil, nil)
if err != nil || got.ID != id {
t.Fatalf("Upsert_Success: err=%v", err)
}
}

func TestUserStore_GetByID_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeUserRowFn(id)}
},
}
s := NewUserStore(pool)
got, err := s.GetByID(context.Background(), id)
if err != nil || got.ID != id {
t.Fatalf("GetByID_Success: err=%v", err)
}
}

func TestUserStore_GetByEmail_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeUserRowFn(id)}
},
}
s := NewUserStore(pool)
got, err := s.GetByEmail(context.Background(), "a@b.com")
if err != nil || got.ID != id {
t.Fatalf("GetByEmail_Success: err=%v", err)
}
}

func TestUserStore_Search_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{
func(dest ...any) error {
*dest[0].(*uuid.UUID) = id
*dest[1].(**string) = nil
*dest[2].(**string) = nil
return nil
},
}}, nil
},
}
s := NewUserStore(pool)
got, err := s.Search(context.Background(), "alice", uuid.New(), nil, 10)
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("Search_Success: err=%v", err)
}
}
