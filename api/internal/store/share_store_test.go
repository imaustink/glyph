package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ─── scanShare ───────────────────────────────────────────────────────────────

func TestScanShare_ScanError(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
	_, err := scanShare(row)
	if err == nil {
		t.Error("expected error")
	}
}

func TestScanShare_ErrNoRows(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	_, err := scanShare(row)
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestShareStore_Create_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewShareStore(pool)
	_, err := s.Create(context.Background(), &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedByID:   uuid.New(),
		Permission:   model.SharePermissionViewer,
	})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── ListForResource ─────────────────────────────────────────────────────────

func TestShareStore_ListForResource_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewShareStore(pool)
	_, err := s.ListForResource(context.Background(), model.ShareResourcePage, uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestShareStore_ListForResource_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewShareStore(pool)
	_, err := s.ListForResource(context.Background(), model.ShareResourcePage, uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestShareStore_ListForResource_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewShareStore(pool)
	_, err := s.ListForResource(context.Background(), model.ShareResourcePage, uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── UpdatePermission ────────────────────────────────────────────────────────

func TestShareStore_UpdatePermission_ExecError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	s := NewShareStore(pool)
	_, err := s.UpdatePermission(context.Background(), uuid.New(), model.SharePermissionViewer)
	if err == nil {
		t.Error("expected error")
	}
}

func TestShareStore_UpdatePermission_GetByIDError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewShareStore(pool)
	_, err := s.UpdatePermission(context.Background(), uuid.New(), model.SharePermissionViewer)
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetForUserAndResource ────────────────────────────────────────────────────

func TestShareStore_GetForUserAndResource_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewShareStore(pool)
_, err := s.GetForUserAndResource(context.Background(), uuid.New(), model.ShareResourcePage, uuid.New())
if err == nil {
t.Error("expected error")
}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestShareStore_Delete_ExecError(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.CommandTag{}, errors.New("exec error")
},
}
s := NewShareStore(pool)
err := s.Delete(context.Background(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

func TestShareStore_Delete_Success(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("DELETE 1"), nil
},
}
s := NewShareStore(pool)
err := s.Delete(context.Background(), uuid.New())
if err != nil {
t.Errorf("expected nil, got %v", err)
}
}

// ─── scanShare success path ───────────────────────────────────────────────────

func makeScanShareFn() func(dest ...any) error {
id := uuid.New()
now := time.Now()
return func(dest ...any) error {
vals := []any{
id,                                    // ID
model.ShareResourcePage,               // ResourceType
uuid.New(),                            // ResourceID
uuid.New(),                            // SharedByID
uuid.New(),                            // SharedWith.ID
(*string)(nil),                        // SharedWith.Email
(*string)(nil),                        // SharedWith.Name
model.SharePermissionViewer,           // Permission
now,                                   // CreatedAt
}
for i, d := range dest {
switch p := d.(type) {
case *uuid.UUID:
if v, ok := vals[i].(uuid.UUID); ok {
*p = v
}
case *model.ShareResourceType:
if v, ok := vals[i].(model.ShareResourceType); ok {
*p = v
}
case *model.SharePermission:
if v, ok := vals[i].(model.SharePermission); ok {
*p = v
}
case **string:
if v, ok := vals[i].(*string); ok {
*p = v
}
case *time.Time:
if v, ok := vals[i].(time.Time); ok {
*p = v
}
}
}
return nil
}
}

func TestScanShare_Success(t *testing.T) {
row := &mockRow{scanFn: makeScanShareFn()}
sh, err := scanShare(row)
if err != nil || sh == nil {
t.Fatalf("scanShare success: %v", err)
}
}

func TestShareStore_GetByID_Success(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeScanShareFn()}
},
}
s := NewShareStore(pool)
sh, err := s.GetByID(context.Background(), uuid.New())
if err != nil || sh == nil {
t.Fatalf("GetByID success: %v", err)
}
}

func TestShareStore_GetForUserAndResource_Success(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeScanShareFn()}
},
}
s := NewShareStore(pool)
sh, err := s.GetForUserAndResource(context.Background(), uuid.New(), model.ShareResourcePage, uuid.New())
if err != nil || sh == nil {
t.Fatalf("GetForUserAndResource success: %v", err)
}
}

func TestShareStore_Create_Success(t *testing.T) {
callCount := 0
shareID := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
callCount++
if callCount == 1 {
// First call: INSERT RETURNING id
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = shareID
return nil
}}
}
// Second call: GetByID select
return &mockRow{scanFn: makeScanShareFn()}
},
}
s := NewShareStore(pool)
sh, err := s.Create(context.Background(), &model.Share{
ResourceType: model.ShareResourcePage,
ResourceID:   uuid.New(),
SharedByID:   uuid.New(),
SharedWith:   model.ShareUser{ID: uuid.New()},
Permission:   model.SharePermissionViewer,
})
if err != nil || sh == nil {
t.Fatalf("Create success: callCount=%d err=%v", callCount, err)
}
}

func TestShareStore_ListForResource_Success(t *testing.T) {
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{makeScanShareFn()}}, nil
},
}
s := NewShareStore(pool)
got, err := s.ListForResource(context.Background(), model.ShareResourcePage, uuid.New())
if err != nil || len(got) != 1 {
t.Fatalf("ListForResource_Success: err=%v", err)
}
}
