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

// helper: fill a valid lane row scan
func laneRowFn(id, userID uuid.UUID, title string, order int) func(dest ...any) error {
	filterJSON := []byte(`{"conjunction":"and","rules":[]}`)
	sortJSON := []byte(`{"mode":"auto"}`)
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = id
		*dest[1].(*uuid.UUID) = userID
		*dest[2].(*string) = title
		*dest[3].(*[]byte) = filterJSON
		*dest[4].(*[]byte) = sortJSON
		*dest[5].(*int) = order
		*dest[6].(**uuid.UUID) = nil
		*dest[7].(*time.Time) = time.Time{}
		*dest[8].(*time.Time) = time.Time{}
		return nil
	}
}

// ─── scanLane ─────────────────────────────────────────────────────────────────

func TestScanLane_ScanError(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
	_, err := scanLane(row)
	if err == nil {
		t.Error("expected error")
	}
}

func TestScanLane_ErrNoRows(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	_, err := scanLane(row)
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestScanLane_BadFilterJSON(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error {
		*dest[0].(*uuid.UUID) = uuid.New()
		*dest[1].(*uuid.UUID) = uuid.New()
		*dest[2].(*string) = "test"
		*dest[3].(*[]byte) = []byte("{bad json")
		*dest[4].(*[]byte) = []byte(`{"mode":"auto"}`)
		*dest[5].(*int) = 0
		*dest[6].(**uuid.UUID) = nil
		*dest[7].(*time.Time) = time.Time{}
		*dest[8].(*time.Time) = time.Time{}
		return nil
	}}
	_, err := scanLane(row)
	if err == nil {
		t.Error("expected unmarshal error for bad filterJSON")
	}
}

func TestScanLane_BadSortJSON(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error {
		*dest[0].(*uuid.UUID) = uuid.New()
		*dest[1].(*uuid.UUID) = uuid.New()
		*dest[2].(*string) = "test"
		*dest[3].(*[]byte) = []byte(`{"conjunction":"and","rules":[]}`)
		*dest[4].(*[]byte) = []byte("{bad json")
		*dest[5].(*int) = 0
		*dest[6].(**uuid.UUID) = nil
		*dest[7].(*time.Time) = time.Time{}
		*dest[8].(*time.Time) = time.Time{}
		return nil
	}}
	_, err := scanLane(row)
	if err == nil {
		t.Error("expected unmarshal error for bad sortJSON")
	}
}

// ─── ListByUser ──────────────────────────────────────────────────────────────

func TestLaneStore_ListByUser_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewLaneStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_ListByUser_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewLaneStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_ListByUser_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewLaneStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Upsert ──────────────────────────────────────────────────────────────────

func TestLaneStore_Upsert_MarshalFilterError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.Upsert(context.Background(), &model.Lane{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_Upsert_MarshalSortError(t *testing.T) {
	callCount := 0
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return nil, errors.New("sort marshal err")
		}
		return json.Marshal(v)
	}
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.Upsert(context.Background(), &model.Lane{})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestLaneStore_Create_MarshalFilterError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.Create(context.Background(), &model.Lane{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_Create_MarshalSortError(t *testing.T) {
	callCount := 0
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return nil, errors.New("sort marshal err")
		}
		return json.Marshal(v)
	}
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.Create(context.Background(), &model.Lane{})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestLaneStore_Update_MarshalFilterError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.Update(context.Background(), &model.Lane{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_Update_MarshalSortError(t *testing.T) {
	callCount := 0
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return nil, errors.New("sort marshal err")
		}
		return json.Marshal(v)
	}
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.Update(context.Background(), &model.Lane{})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestLaneStore_Delete_ExecError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	s := NewLaneStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_Delete_NotFound(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	s := NewLaneStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── ReorderAll ──────────────────────────────────────────────────────────────

func TestLaneStore_ReorderAll_BeginError(t *testing.T) {
	pool := &mockPool{
		beginFn: func(ctx context.Context) (pgx.Tx, error) {
			return nil, errors.New("begin error")
		},
	}
	s := NewLaneStore(pool)
	err := s.ReorderAll(context.Background(), uuid.New(), []LaneReorderItem{{ID: uuid.New(), Order: 0}})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_ReorderAll_ExecError(t *testing.T) {
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	pool := &mockPool{
		beginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
	}
	s := NewLaneStore(pool)
	err := s.ReorderAll(context.Background(), uuid.New(), []LaneReorderItem{{ID: uuid.New(), Order: 0}})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── BatchCreate ─────────────────────────────────────────────────────────────

func TestLaneStore_BatchCreate_Empty(t *testing.T) {
	s := NewLaneStore(&mockPool{})
	result, err := s.BatchCreate(context.Background(), []*model.Lane{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result")
	}
}

func TestLaneStore_BatchCreate_BeginError(t *testing.T) {
	pool := &mockPool{
		beginFn: func(ctx context.Context) (pgx.Tx, error) {
			return nil, errors.New("begin error")
		},
	}
	s := NewLaneStore(pool)
	_, err := s.BatchCreate(context.Background(), []*model.Lane{{UserID: uuid.New()}})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_BatchCreate_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewLaneStore(&mockPool{})
	_, err := s.BatchCreate(context.Background(), []*model.Lane{{UserID: uuid.New()}})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_BatchCreate_ScanError(t *testing.T) {
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	pool := &mockPool{
		beginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
	}
	s := NewLaneStore(pool)
	_, err := s.BatchCreate(context.Background(), []*model.Lane{{UserID: uuid.New()}})
	if err == nil {
		t.Error("expected error")
	}
}

func TestLaneStore_BatchCreate_CommitError(t *testing.T) {
	id := uuid.New()
	uid := uuid.New()
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: laneRowFn(id, uid, "test", 0)}
		},
		commitErr: errors.New("commit error"),
	}
	pool := &mockPool{
		beginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
	}
	s := NewLaneStore(pool)
	_, err := s.BatchCreate(context.Background(), []*model.Lane{{UserID: uid}})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetByID ──────────────────────────────────────────────────────────────────

func TestLaneStore_GetByID_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewLaneStore(pool)
_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

func TestLaneStore_GetByID_NotFound(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
},
}
s := NewLaneStore(pool)
_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err != ErrNotFound {
t.Errorf("want ErrNotFound, got %v", err)
}
}

// ─── Upsert ScanError ─────────────────────────────────────────────────────────

func TestLaneStore_Upsert_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewLaneStore(pool)
_, err := s.Upsert(context.Background(), &model.Lane{UserID: uuid.New()})
if err == nil {
t.Error("expected error")
}
}

// ─── Create ScanError ─────────────────────────────────────────────────────────

func TestLaneStore_Create_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewLaneStore(pool)
_, err := s.Create(context.Background(), &model.Lane{UserID: uuid.New()})
if err == nil {
t.Error("expected error")
}
}

// ─── Update ScanError ─────────────────────────────────────────────────────────

func TestLaneStore_Update_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewLaneStore(pool)
_, err := s.Update(context.Background(), &model.Lane{ID: uuid.New(), UserID: uuid.New()})
if err == nil {
t.Error("expected error")
}
}

// ─── Success paths ────────────────────────────────────────────────────────────

func TestScanLane_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
row := &mockRow{scanFn: laneRowFn(id, uid, "title", 0)}
l, err := scanLane(row)
if err != nil || l.ID != id {
t.Fatalf("scanLane success: err=%v", err)
}
}

func TestLaneStore_ListByUser_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{laneRowFn(id, uid, "t", 0)}}, nil
},
}
s := NewLaneStore(pool)
got, err := s.ListByUser(context.Background(), uid)
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("ListByUser_Success: err=%v", err)
}
}

func TestLaneStore_GetByID_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: laneRowFn(id, uid, "t", 0)}
},
}
s := NewLaneStore(pool)
got, err := s.GetByID(context.Background(), id, uid)
if err != nil || got.ID != id {
t.Fatalf("GetByID_Success: err=%v", err)
}
}

func TestLaneStore_Upsert_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: laneRowFn(id, uid, "t", 0)}
},
}
s := NewLaneStore(pool)
got, err := s.Upsert(context.Background(), &model.Lane{ID: id, UserID: uid})
if err != nil || got.ID != id {
t.Fatalf("Upsert_Success: err=%v", err)
}
}

func TestLaneStore_Create_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: laneRowFn(id, uid, "t", 0)}
},
}
s := NewLaneStore(pool)
got, err := s.Create(context.Background(), &model.Lane{UserID: uid})
if err != nil || got.ID != id {
t.Fatalf("Create_Success: err=%v", err)
}
}

func TestLaneStore_Update_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: laneRowFn(id, uid, "t", 0)}
},
}
s := NewLaneStore(pool)
got, err := s.Update(context.Background(), &model.Lane{ID: id, UserID: uid})
if err != nil || got.ID != id {
t.Fatalf("Update_Success: err=%v", err)
}
}

func TestLaneStore_Delete_Success(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("DELETE 1"), nil
},
}
s := NewLaneStore(pool)
err := s.Delete(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Fatalf("Delete_Success: err=%v", err)
}
}

func TestLaneStore_ReorderAll_Success(t *testing.T) {
tx := &mockTx{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("UPDATE 1"), nil
},
}
pool := &mockPool{
beginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
}
s := NewLaneStore(pool)
err := s.ReorderAll(context.Background(), uuid.New(), []LaneReorderItem{{ID: uuid.New(), Order: 1}})
if err != nil {
t.Fatalf("ReorderAll_Success: err=%v", err)
}
}

func TestLaneStore_BatchCreate_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
tx := &mockTx{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: laneRowFn(id, uid, "t", 0)}
},
}
pool := &mockPool{
beginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
}
s := NewLaneStore(pool)
got, err := s.BatchCreate(context.Background(), []*model.Lane{{UserID: uid}})
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("BatchCreate_Success: err=%v", err)
}
}

func TestLaneStore_BatchCreate_MarshalSortError(t *testing.T) {
callCount := 0
orig := jsonMarshal
jsonMarshal = func(v any) ([]byte, error) {
callCount++
if callCount == 2 {
return nil, errors.New("sort marshal err")
}
return orig(v)
}
defer func() { jsonMarshal = orig }()

tx := &mockTx{}
pool := &mockPool{
beginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
}
s := NewLaneStore(pool)
_, err := s.BatchCreate(context.Background(), []*model.Lane{{UserID: uuid.New()}})
if err == nil {
t.Error("expected error on sort marshal")
}
}
