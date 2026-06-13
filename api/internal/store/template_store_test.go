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

func templateRowFn(id, userID uuid.UUID) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = id
		*dest[1].(*uuid.UUID) = userID
		*dest[2].(*string) = "name"
		*dest[3].(*string) = "content"
		*dest[4].(*string) = "" // title_template (string, not *string)
		*dest[5].(*[]byte) = nil // todo_trigger
		*dest[6].(**uuid.UUID) = nil // default_folder_id
		*dest[7].(*bool) = false    // is_default
		*dest[8].(**uuid.UUID) = nil // org_id
		*dest[9].(*bool) = false     // is_private
		*dest[10].(*time.Time) = time.Time{}
		*dest[11].(*time.Time) = time.Time{}
		return nil
	}
}

// ─── scanTemplate ────────────────────────────────────────────────────────────

func TestScanTemplate_ScanError(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
	_, err := scanTemplate(row)
	if err == nil {
		t.Error("expected error")
	}
}

func TestScanTemplate_ErrNoRows(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	_, err := scanTemplate(row)
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestScanTemplate_BadTriggerJSON(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error {
		*dest[0].(*uuid.UUID) = uuid.New()
		*dest[1].(*uuid.UUID) = uuid.New()
		*dest[2].(*string) = "name"
		*dest[3].(*string) = "content"
		*dest[4].(*string) = ""
		*dest[5].(*[]byte) = []byte("{bad json")
		*dest[6].(**uuid.UUID) = nil
		*dest[7].(*bool) = false
		*dest[8].(**uuid.UUID) = nil
		*dest[9].(*bool) = false
		*dest[10].(*time.Time) = time.Time{}
		*dest[11].(*time.Time) = time.Time{}
		return nil
	}}
	_, err := scanTemplate(row)
	if err == nil {
		t.Error("expected unmarshal error for bad triggerJSON")
	}
}

// ─── unmarshalJSON ───────────────────────────────────────────────────────────

func TestUnmarshalJSON_BadData(t *testing.T) {
	var v model.TodoTriggerConfig
	err := unmarshalJSON([]byte("{bad json"), &v)
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

// ─── ListByUser ──────────────────────────────────────────────────────────────

func TestTemplateStore_ListByUser_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewTemplateStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTemplateStore_ListByUser_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewTemplateStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTemplateStore_ListByUser_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewTemplateStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestTemplateStore_Delete_ExecError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	s := NewTemplateStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTemplateStore_Delete_NotFound(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	s := NewTemplateStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestTemplateStore_Create_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewTemplateStore(pool)
	_, err := s.Create(context.Background(), &model.Template{UserID: uuid.New()})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestTemplateStore_Update_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewTemplateStore(&mockPool{})
	trigger := &model.TodoTriggerConfig{}
	_, err := s.Update(context.Background(), &model.Template{TodoTrigger: trigger})
	if err == nil {
		t.Error("expected marshal error")
	}
}

// ─── Upsert ──────────────────────────────────────────────────────────────────

func TestTemplateStore_Upsert_MarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
	defer func() { jsonMarshal = orig }()

	s := NewTemplateStore(&mockPool{})
	trigger := &model.TodoTriggerConfig{}
	_, err := s.Upsert(context.Background(), &model.Template{TodoTrigger: trigger})
	if err == nil {
		t.Error("expected marshal error")
	}
}

// ─── GetByID ──────────────────────────────────────────────────────────────────

func TestTemplateStore_GetByID_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewTemplateStore(pool)
_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err == nil {
t.Error("expected error")
}
}

func TestTemplateStore_GetByID_NotFound(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
},
}
s := NewTemplateStore(pool)
_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err != ErrNotFound {
t.Errorf("want ErrNotFound, got %v", err)
}
}

// ─── Upsert scan error ────────────────────────────────────────────────────────

func TestTemplateStore_Upsert_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewTemplateStore(pool)
_, err := s.Upsert(context.Background(), &model.Template{Name: "t"})
if err == nil {
t.Error("expected error")
}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func TestTemplateStore_Update_ScanError(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
},
}
s := NewTemplateStore(pool)
_, err := s.Update(context.Background(), &model.Template{ID: uuid.New(), UserID: uuid.New()})
if err == nil {
t.Error("expected error")
}
}

func TestTemplateStore_Update_NotFound(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
},
}
s := NewTemplateStore(pool)
_, err := s.Update(context.Background(), &model.Template{ID: uuid.New(), UserID: uuid.New()})
if err != ErrNotFound {
t.Errorf("want ErrNotFound, got %v", err)
}
}

// ─── unmarshalJSON ────────────────────────────────────────────────────────────

func TestUnmarshalJSON_InvalidJSON(t *testing.T) {
var v map[string]any
err := unmarshalJSON([]byte("{invalid}"), &v)
if err == nil {
t.Error("expected error for invalid JSON")
}
}

func TestUnmarshalJSON_ValidJSON(t *testing.T) {
var v map[string]any
err := unmarshalJSON([]byte(`{"key":"value"}`), &v)
if err != nil {
t.Errorf("unexpected error: %v", err)
}
}


// ─── Success paths ────────────────────────────────────────────────────────────

func TestScanTemplate_NilTrigger(t *testing.T) {
id, uid := uuid.New(), uuid.New()
row := &mockRow{scanFn: templateRowFn(id, uid)}
tmpl, err := scanTemplate(row)
if err != nil || tmpl.ID != id || tmpl.TodoTrigger != nil {
t.Fatalf("scanTemplate nil trigger: err=%v", err)
}
}

func TestTemplateStore_ListByUser_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{templateRowFn(id, uid)}}, nil
},
}
s := NewTemplateStore(pool)
got, err := s.ListByUser(context.Background(), uid)
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("ListByUser_Success: err=%v", err)
}
}

func TestTemplateStore_GetByID_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: templateRowFn(id, uid)}
},
}
s := NewTemplateStore(pool)
got, err := s.GetByID(context.Background(), id, uid)
if err != nil || got.ID != id {
t.Fatalf("GetByID_Success: err=%v", err)
}
}

func TestTemplateStore_Upsert_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: templateRowFn(id, uid)}
},
}
s := NewTemplateStore(pool)
got, err := s.Upsert(context.Background(), &model.Template{ID: id, UserID: uid})
if err != nil || got.ID != id {
t.Fatalf("Upsert_Success: err=%v", err)
}
}

func TestTemplateStore_Create_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: templateRowFn(id, uid)}
},
}
s := NewTemplateStore(pool)
got, err := s.Create(context.Background(), &model.Template{UserID: uid})
if err != nil || got.ID != id {
t.Fatalf("Create_Success: err=%v", err)
}
}

func TestTemplateStore_Update_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: templateRowFn(id, uid)}
},
}
s := NewTemplateStore(pool)
got, err := s.Update(context.Background(), &model.Template{ID: id, UserID: uid})
if err != nil || got.ID != id {
t.Fatalf("Update_Success: err=%v", err)
}
}

func TestTemplateStore_Delete_Success(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("DELETE 1"), nil
},
}
s := NewTemplateStore(pool)
err := s.Delete(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Fatalf("Delete_Success: err=%v", err)
}
}

func TestTemplateStore_Create_MarshalError(t *testing.T) {
orig := jsonMarshal
jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal err") }
defer func() { jsonMarshal = orig }()

trigger := &model.TodoTriggerConfig{Pattern: "TODO"}
s := NewTemplateStore(&mockPool{})
_, err := s.Create(context.Background(), &model.Template{UserID: uuid.New(), TodoTrigger: trigger})
if err == nil {
t.Error("expected error")
}
}
