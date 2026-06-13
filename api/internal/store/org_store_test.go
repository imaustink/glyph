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

// ─── Create ──────────────────────────────────────────────────────────────────

func TestOrgStore_Create_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.Create(context.Background(), &model.Organization{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetByID ─────────────────────────────────────────────────────────────────

func TestOrgStore_GetByID_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.GetByID(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── ListForUser ─────────────────────────────────────────────────────────────

func TestOrgStore_ListForUser_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewOrgStore(pool)
	_, err := s.ListForUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_ListForUser_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewOrgStore(pool)
	_, err := s.ListForUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_ListForUser_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewOrgStore(pool)
	_, err := s.ListForUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestOrgStore_Update_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.Update(context.Background(), &model.Organization{ID: uuid.New()})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── AddMember ───────────────────────────────────────────────────────────────

func TestOrgStore_AddMember_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.AddMember(context.Background(), uuid.New(), uuid.New(), model.OrgRoleViewer)
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetMember ───────────────────────────────────────────────────────────────

func TestOrgStore_GetMember_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.GetMember(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_GetMember_NotFound(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.GetMember(context.Background(), uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── ListMembers ─────────────────────────────────────────────────────────────

func TestOrgStore_ListMembers_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewOrgStore(pool)
	_, err := s.ListMembers(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_ListMembers_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewOrgStore(pool)
	_, err := s.ListMembers(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_ListMembers_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewOrgStore(pool)
	_, err := s.ListMembers(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── UpdateMemberRole ────────────────────────────────────────────────────────

func TestOrgStore_UpdateMemberRole_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewOrgStore(pool)
	_, err := s.UpdateMemberRole(context.Background(), uuid.New(), uuid.New(), model.OrgRoleViewer)
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetUserOrgIDs ───────────────────────────────────────────────────────────

func TestOrgStore_GetUserOrgIDs_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewOrgStore(pool)
	_, err := s.GetUserOrgIDs(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_GetUserOrgIDs_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewOrgStore(pool)
	_, err := s.GetUserOrgIDs(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestOrgStore_GetUserOrgIDs_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewOrgStore(pool)
	_, err := s.GetUserOrgIDs(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestOrgStore_Delete_ExecError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	s := NewOrgStore(pool)
	err := s.Delete(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── RemoveMember ─────────────────────────────────────────────────────────────

func TestOrgStore_RemoveMember_ExecError(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.CommandTag{}, errors.New("exec error")
},
}
s := NewOrgStore(pool)
err := s.RemoveMember(context.Background(), uuid.New(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

func TestOrgStore_RemoveMember_Success(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("DELETE 1"), nil
},
}
s := NewOrgStore(pool)
err := s.RemoveMember(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Errorf("expected nil, got %v", err)
}
}

// ─── AddMember ScanError ──────────────────────────────────────────────────────
// ─── AddMember success (2 QueryRow calls) ─────────────────────────────────────

func TestOrgStore_AddMember_Success(t *testing.T) {
orgID := uuid.New()
userID := uuid.New()
callCount := 0
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
callCount++
if callCount == 1 {
// First call: INSERT RETURNING org_id, user_id, role, joined_at
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = orgID
*dest[1].(*uuid.UUID) = userID
*dest[2].(*model.OrgRole) = model.OrgRoleViewer
*dest[3].(*time.Time) = time.Now()
return nil
}}
}
// Second call: SELECT email, name FROM users (hydration)
return &mockRow{scanFn: func(dest ...any) error { return errors.New("no user") }}
},
}
s := NewOrgStore(pool)
m, err := s.AddMember(context.Background(), orgID, userID, model.OrgRoleViewer)
if err != nil || m == nil {
t.Fatalf("AddMember success: callCount=%d err=%v", callCount, err)
}
if m.UserID != userID {
t.Errorf("unexpected userID: %v", m.UserID)
}
}

// ─── UpdateMemberRole success (2 QueryRow calls) ─────────────────────────────

func TestOrgStore_UpdateMemberRole_Success(t *testing.T) {
orgID := uuid.New()
userID := uuid.New()
callCount := 0
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
callCount++
if callCount == 1 {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = orgID
*dest[1].(*uuid.UUID) = userID
*dest[2].(*model.OrgRole) = model.OrgRoleOwner
*dest[3].(*time.Time) = time.Now()
return nil
}}
}
return &mockRow{scanFn: func(dest ...any) error { return errors.New("no user") }}
},
}
s := NewOrgStore(pool)
m, err := s.UpdateMemberRole(context.Background(), orgID, userID, model.OrgRoleOwner)
if err != nil || m == nil {
t.Fatalf("UpdateMemberRole success: callCount=%d err=%v", callCount, err)
}
if m.Role != model.OrgRoleOwner {
t.Errorf("unexpected role: %v", m.Role)
}
}

// ─── Success paths ────────────────────────────────────────────────────────────

func TestOrgStore_Create_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = id
*dest[1].(*string) = "test"
*dest[2].(*uuid.UUID) = uuid.New()
*dest[3].(*time.Time) = time.Time{}
*dest[4].(*time.Time) = time.Time{}
return nil
}}
},
}
s := NewOrgStore(pool)
got, err := s.Create(context.Background(), &model.Organization{Name: "test"})
if err != nil || got.ID != id {
t.Fatalf("Create_Success: err=%v", err)
}
}

func TestOrgStore_GetByID_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = id
*dest[1].(*string) = "test"
*dest[2].(*uuid.UUID) = uuid.New()
*dest[3].(*int) = 1
*dest[4].(*time.Time) = time.Time{}
*dest[5].(*time.Time) = time.Time{}
return nil
}}
},
}
s := NewOrgStore(pool)
got, err := s.GetByID(context.Background(), id)
if err != nil || got.ID != id {
t.Fatalf("GetByID_Success: err=%v", err)
}
}

func TestOrgStore_ListForUser_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{
func(dest ...any) error {
*dest[0].(*uuid.UUID) = id
*dest[1].(*string) = "test"
*dest[2].(*uuid.UUID) = uuid.New()
*dest[3].(*int) = 1
*dest[4].(*time.Time) = time.Time{}
*dest[5].(*time.Time) = time.Time{}
*dest[6].(*model.OrgRole) = model.OrgRoleOwner
return nil
},
}}, nil
},
}
s := NewOrgStore(pool)
got, err := s.ListForUser(context.Background(), uuid.New())
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("ListForUser_Success: err=%v got=%v", err, got)
}
}

func TestOrgStore_Update_Success(t *testing.T) {
id := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = id
*dest[1].(*string) = "updated"
*dest[2].(*uuid.UUID) = uuid.New()
*dest[3].(*time.Time) = time.Time{}
*dest[4].(*time.Time) = time.Time{}
return nil
}}
},
}
s := NewOrgStore(pool)
got, err := s.Update(context.Background(), &model.Organization{ID: id, Name: "updated"})
if err != nil || got.ID != id {
t.Fatalf("Update_Success: err=%v", err)
}
}

func TestOrgStore_GetMember_Success(t *testing.T) {
orgID, userID := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = orgID
*dest[1].(*uuid.UUID) = userID
*dest[2].(**string) = nil
*dest[3].(**string) = nil
*dest[4].(*model.OrgRole) = model.OrgRoleViewer
*dest[5].(*time.Time) = time.Time{}
return nil
}}
},
}
s := NewOrgStore(pool)
got, err := s.GetMember(context.Background(), orgID, userID)
if err != nil || got.UserID != userID {
t.Fatalf("GetMember_Success: err=%v", err)
}
}

func TestOrgStore_ListMembers_Success(t *testing.T) {
userID := uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{
func(dest ...any) error {
*dest[0].(*uuid.UUID) = uuid.New()
*dest[1].(*uuid.UUID) = userID
*dest[2].(**string) = nil
*dest[3].(**string) = nil
*dest[4].(*model.OrgRole) = model.OrgRoleViewer
*dest[5].(*time.Time) = time.Time{}
return nil
},
}}, nil
},
}
s := NewOrgStore(pool)
got, err := s.ListMembers(context.Background(), uuid.New())
if err != nil || len(got) != 1 {
t.Fatalf("ListMembers_Success: err=%v", err)
}
}

func TestOrgStore_GetUserOrgIDs_Success(t *testing.T) {
orgID := uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{
func(dest ...any) error {
*dest[0].(*uuid.UUID) = orgID
return nil
},
}}, nil
},
}
s := NewOrgStore(pool)
got, err := s.GetUserOrgIDs(context.Background(), uuid.New())
if err != nil || len(got) != 1 || got[0] != orgID {
t.Fatalf("GetUserOrgIDs_Success: err=%v", err)
}
}
