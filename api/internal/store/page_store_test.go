package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func pageRowFn(id, userID uuid.UUID) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = id
		*dest[1].(*uuid.UUID) = userID
		*dest[2].(*model.TreeNodeType) = model.NodeTypePage
		*dest[3].(*string) = "title"
		*dest[4].(**uuid.UUID) = nil // parent_id
		*dest[5].(*int) = 0
		*dest[6].(*[]string) = []string{}
		*dest[7].(*[]byte) = nil // todo_trigger
		*dest[8].(**uuid.UUID) = nil // org_id
		*dest[9].(*bool) = false
		*dest[10].(*time.Time) = time.Time{}
		*dest[11].(*time.Time) = time.Time{}
		return nil
	}
}

// ─── scanPage ─────────────────────────────────────────────────────────────────

func TestScanPage_ScanError(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
	_, err := scanPage(row)
	if err == nil {
		t.Error("expected error")
	}
}

func TestScanPage_ErrNoRows(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	_, err := scanPage(row)
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestScanPage_BadTriggerJSON(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error {
		*dest[0].(*uuid.UUID) = uuid.New()
		*dest[1].(*uuid.UUID) = uuid.New()
		*dest[2].(*model.TreeNodeType) = model.NodeTypePage
		*dest[3].(*string) = "title"
		*dest[4].(**uuid.UUID) = nil
		*dest[5].(*int) = 0
		*dest[6].(*[]string) = []string{}
		*dest[7].(*[]byte) = []byte("{bad json")
		*dest[8].(**uuid.UUID) = nil
		*dest[9].(*bool) = false
		*dest[10].(*time.Time) = time.Time{}
		*dest[11].(*time.Time) = time.Time{}
		return nil
	}}
	_, err := scanPage(row)
	if err == nil {
		t.Error("expected unmarshal error for bad triggerJSON")
	}
}

// ─── ListByUser ──────────────────────────────────────────────────────────────

func TestPageStore_ListByUser_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewPageStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestPageStore_ListByUser_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewPageStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestPageStore_ListByUser_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewPageStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── ListByUserPaginated ─────────────────────────────────────────────────────

func TestPageStore_ListByUserPaginated_CountError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("count error") }}
		},
	}
	s := NewPageStore(pool)
	_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10})
	if err == nil {
		t.Error("expected error")
	}
}

func TestPageStore_ListByUserPaginated_QueryError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("query error")
		},
	}
	s := NewPageStore(pool)
	_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10})
	if err == nil {
		t.Error("expected error")
	}
}

func TestPageStore_ListByUserPaginated_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewPageStore(pool)
	_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestPageStore_Delete_ExecError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	s := NewPageStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestPageStore_Delete_NotFound(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	s := NewPageStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── IsAncestor ──────────────────────────────────────────────────────────────

func TestPageStore_IsAncestor_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewPageStore(pool)
	_, err := s.IsAncestor(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetContent ──────────────────────────────────────────────────────────────

func TestPageStore_GetContent_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewPageStore(pool)
	_, err := s.GetContent(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── UpsertContent ───────────────────────────────────────────────────────────

func TestPageStore_UpsertContent_ExistsCheckError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("db error") }}
		},
	}
	s := NewPageStore(pool)
	_, err := s.UpsertContent(context.Background(), &model.PageContent{PageID: uuid.New()}, uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestPageStore_UpsertContent_NotExists(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error {
				*dest[0].(*bool) = false
				return nil
			}}
		},
	}
	s := NewPageStore(pool)
	_, err := s.UpsertContent(context.Background(), &model.PageContent{PageID: uuid.New()}, uuid.New())
	if err == nil {
		t.Error("expected forbidden error")
	}
}

func TestPageStore_UpsertContent_UpsertError(t *testing.T) {
	callCount := 0
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			callCount++
			if callCount == 1 {
				// EXISTS check
				return &mockRow{scanFn: func(dest ...any) error {
					*dest[0].(*bool) = true
					return nil
				}}
			}
			// upsert query
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("upsert error") }}
		},
	}
	s := NewPageStore(pool)
	_, err := s.UpsertContent(context.Background(), &model.PageContent{PageID: uuid.New()}, uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Upsert ──────────────────────────────────────────────────────────────────

func TestPageStore_Upsert_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewPageStore(&mockPool{})
	trigger := &model.TodoTriggerConfig{}
	_, err := s.Upsert(context.Background(), &model.Page{TodoTrigger: trigger})
	if err == nil {
		t.Error("expected marshal error")
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestPageStore_Create_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewPageStore(&mockPool{})
	trigger := &model.TodoTriggerConfig{}
	_, err := s.Create(context.Background(), &model.Page{TodoTrigger: trigger})
	if err == nil {
		t.Error("expected marshal error")
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestPageStore_Update_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewPageStore(&mockPool{})
	trigger := &model.TodoTriggerConfig{}
	_, err := s.Update(context.Background(), &model.Page{TodoTrigger: trigger})
	if err == nil {
		t.Error("expected marshal error")
	}
}

// ─── marshalNullableJSON ─────────────────────────────────────────────────────

func TestMarshalNullableJSON_NilInput(t *testing.T) {
	b, err := marshalNullableJSON(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if b != nil {
		t.Errorf("expected nil bytes for nil input")
	}
}

func TestMarshalNullableJSON_NonNilInput(t *testing.T) {
	type S struct{ X int }
	b, err := marshalNullableJSON(&S{X: 42})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(b) == 0 {
		t.Error("expected non-empty bytes")
	}
}

func TestMarshalNullableJSON_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	type S struct{ X int }
	_, err := marshalNullableJSON(&S{X: 1})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetByID ──────────────────────────────────────────────────────────────────

func TestPageStore_GetByID_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewPageStore(pool)
_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

func TestPageStore_GetByID_NotFound(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
},
}
s := NewPageStore(pool)
_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err != ErrNotFound {
t.Errorf("want ErrNotFound, got %v", err)
}
}

// ─── Upsert ───────────────────────────────────────────────────────────────────

func TestPageStore_Upsert_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewPageStore(pool)
_, err := s.Upsert(context.Background(), &model.Page{UserID: uuid.New(), Type: model.NodeTypePage})
if err == nil {
t.Error("expected error")
}
}

// ─── Create ───────────────────────────────────────────────────────────────────

func TestPageStore_Create_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewPageStore(pool)
_, err := s.Create(context.Background(), &model.Page{UserID: uuid.New(), Type: model.NodeTypePage})
if err == nil {
t.Error("expected error")
}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func TestPageStore_Update_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewPageStore(pool)
_, err := s.Update(context.Background(), &model.Page{ID: uuid.New(), UserID: uuid.New(), Type: model.NodeTypePage})
if err == nil {
t.Error("expected error")
}
}

// ─── GetContent ───────────────────────────────────────────────────────────────

func TestPageStore_GetContent_NoRows(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
},
}
s := NewPageStore(pool)
_, err := s.GetContent(context.Background(), uuid.New(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

// ─── IsAncestor ───────────────────────────────────────────────────────────────

func TestPageStore_IsAncestor_ScanErr(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
},
}
s := NewPageStore(pool)
_, err := s.IsAncestor(context.Background(), uuid.New(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

// ─── GetContent happy path ────────────────────────────────────────────────────

func TestPageStore_GetContent_Success(t *testing.T) {
pageID := uuid.New()
now := time.Now()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
for i, d := range dest {
switch p := d.(type) {
case *uuid.UUID:
*p = pageID
case *json.RawMessage:
*p = json.RawMessage(`{"type":"doc"}`)
case *time.Time:
*p = now
case *int:
*p = 1
}
_ = i
}
return nil
}}
},
}
s := NewPageStore(pool)
pc, err := s.GetContent(context.Background(), pageID, uuid.New())
if err != nil {
t.Errorf("unexpected error: %v", err)
}
if pc == nil {
t.Error("expected non-nil content")
}
}

// ─── ListByUserPaginated ScanError ───────────────────────────────────────────

func TestPageStore_ListByUserPaginated_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*int) = 0
return nil
}}
},
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{
rows: []func(dest ...any) error{
func(dest ...any) error { return errors.New("scan err") },
},
}, nil
},
}
s := NewPageStore(pool)
_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10, Offset: 0})
if err == nil {
t.Error("expected error")
}
}

// ─── Success paths ────────────────────────────────────────────────────────────

func TestScanPage_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
row := &mockRow{scanFn: pageRowFn(id, uid)}
p, err := scanPage(row)
if err != nil || p.ID != id {
t.Fatalf("scanPage success: err=%v", err)
}
}

func TestPageStore_ListByUser_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{pageRowFn(id, uid)}}, nil
},
}
s := NewPageStore(pool)
got, err := s.ListByUser(context.Background(), uid)
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("ListByUser_Success: err=%v", err)
}
}

func TestPageStore_ListByUserPaginated_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
callCount := 0
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
callCount++
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*int) = 1
return nil
}}
},
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{pageRowFn(id, uid)}}, nil
},
}
s := NewPageStore(pool)
got, total, err := s.ListByUserPaginated(context.Background(), uid, Pagination{Limit: 10})
if err != nil || len(got) != 1 || total != 1 {
t.Fatalf("ListByUserPaginated_Success: err=%v got=%v total=%d", err, got, total)
}
}

func TestPageStore_GetByID_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: pageRowFn(id, uid)}
},
}
s := NewPageStore(pool)
got, err := s.GetByID(context.Background(), id, uid)
if err != nil || got.ID != id {
t.Fatalf("GetByID_Success: err=%v", err)
}
}

func TestPageStore_Upsert_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: pageRowFn(id, uid)}
},
}
s := NewPageStore(pool)
got, err := s.Upsert(context.Background(), &model.Page{ID: id, UserID: uid, Type: model.NodeTypePage})
if err != nil || got.ID != id {
t.Fatalf("Upsert_Success: err=%v", err)
}
}

func TestPageStore_Create_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: pageRowFn(id, uid)}
},
}
s := NewPageStore(pool)
got, err := s.Create(context.Background(), &model.Page{UserID: uid, Type: model.NodeTypePage})
if err != nil || got.ID != id {
t.Fatalf("Create_Success: err=%v", err)
}
}

func TestPageStore_Update_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: pageRowFn(id, uid)}
},
}
s := NewPageStore(pool)
got, err := s.Update(context.Background(), &model.Page{ID: id, UserID: uid, Type: model.NodeTypePage})
if err != nil || got.ID != id {
t.Fatalf("Update_Success: err=%v", err)
}
}

func TestPageStore_Delete_Success(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("DELETE 1"), nil
},
}
s := NewPageStore(pool)
err := s.Delete(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Fatalf("Delete_Success: err=%v", err)
}
}

func TestPageStore_IsAncestor_Success(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*bool) = true
return nil
}}
},
}
s := NewPageStore(pool)
got, err := s.IsAncestor(context.Background(), uuid.New(), uuid.New())
if err != nil || !got {
t.Fatalf("IsAncestor_Success: err=%v got=%v", err, got)
}
}

func TestPageStore_GetContent_NilContent(t *testing.T) {
pageID := uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = pageID
*dest[1].(*json.RawMessage) = nil
*dest[2].(*time.Time) = time.Time{}
*dest[3].(*int) = 0
return nil
}}
},
}
s := NewPageStore(pool)
got, err := s.GetContent(context.Background(), pageID, uuid.New())
if err != nil || got.Content == nil {
t.Fatalf("GetContent_NilContent: err=%v content=%v", err, got)
}
}

func TestPageStore_UpsertContent_Success(t *testing.T) {
pageID := uuid.New()
callCount := 0
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
callCount++
if callCount == 1 {
// EXISTS check
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*bool) = true
return nil
}}
}
// INSERT RETURNING
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*uuid.UUID) = pageID
*dest[1].(*json.RawMessage) = json.RawMessage(`{"type":"doc","content":[]}`)
*dest[2].(*time.Time) = time.Time{}
*dest[3].(*int) = 0
return nil
}}
},
}
s := NewPageStore(pool)
got, err := s.UpsertContent(context.Background(), &model.PageContent{
PageID:  pageID,
Content: json.RawMessage(`{"type":"doc","content":[]}`),
}, uuid.New())
if err != nil || got.PageID != pageID {
t.Fatalf("UpsertContent_Success: err=%v", err)
}
}
