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

func taskRowFn(id, userID uuid.UUID) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = id
		*dest[1].(*uuid.UUID) = userID
		*dest[2].(*string) = "title"
		*dest[3].(*string) = "" // description (string, not *string)
		*dest[4].(*model.TaskStatus) = model.StatusTodo
		*dest[5].(*model.Priority) = model.PriorityNone
		*dest[6].(*[]string) = []string{}
		*dest[7].(**string) = nil    // due_date
		*dest[8].(**uuid.UUID) = nil // source_page_id
		*dest[9].(**string) = nil    // source_node_id
		*dest[10].(*[]byte) = nil    // link JSON
		*dest[11].(*int) = 0
		*dest[12].(**uuid.UUID) = nil // org_id
		*dest[13].(*bool) = false
		*dest[14].(**uuid.UUID) = nil // folder_id
		*dest[15].(*time.Time) = time.Time{}
		*dest[16].(*time.Time) = time.Time{}
		return nil
	}
}

// ─── scanTask ─────────────────────────────────────────────────────────────────

func TestScanTask_ScanError(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
	_, err := scanTask(row)
	if err == nil {
		t.Error("expected error")
	}
}

func TestScanTask_ErrNoRows(t *testing.T) {
	row := &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
	_, err := scanTask(row)
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── ListByUser ──────────────────────────────────────────────────────────────

func TestTaskStore_ListByUser_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByUser_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByUser_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListByUser(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── ListByUserPaginated ─────────────────────────────────────────────────────

func TestTaskStore_ListByUserPaginated_CountError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("count error") }}
		},
	}
	s := NewTaskStore(pool)
	_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10})
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByUserPaginated_QueryError(t *testing.T) {
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
	s := NewTaskStore(pool)
	_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10})
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByUserPaginated_RowsErr(t *testing.T) {
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
	s := NewTaskStore(pool)
	_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── ListBySourcePage ────────────────────────────────────────────────────────

func TestTaskStore_ListBySourcePage_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListBySourcePage(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListBySourcePage_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListBySourcePage(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListBySourcePage_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListBySourcePage(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

// ─── ListBySourceNode ────────────────────────────────────────────────────────

func TestTaskStore_ListBySourceNode_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListBySourceNode(context.Background(), uuid.New(), "node-id")
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListBySourceNode_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListBySourceNode(context.Background(), uuid.New(), "node-id")
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListBySourceNode_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListBySourceNode(context.Background(), uuid.New(), "node-id")
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestTaskStore_Delete_ExecError(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec error")
		},
	}
	s := NewTaskStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_Delete_NotFound(t *testing.T) {
	pool := &mockPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	s := NewTaskStore(pool)
	err := s.Delete(context.Background(), uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── ListByFilter ────────────────────────────────────────────────────────────

func TestTaskStore_ListByFilter_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListByFilter(context.Background(), uuid.New(), model.FilterSet{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByFilter_WithFilter_QueryError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("db error")
		},
	}
	s := NewTaskStore(pool)
	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{{Field: "status", Operator: model.FilterOpEq, Value: "done"}},
	}
	_, err := s.ListByFilter(context.Background(), uuid.New(), fs)
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByFilter_RowsErr(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{err: errors.New("rows err")}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListByFilter(context.Background(), uuid.New(), model.FilterSet{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_ListByFilter_ScanError(t *testing.T) {
	pool := &mockPool{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []func(...any) error{
				func(dest ...any) error { return errors.New("scan error") },
			}}, nil
		},
	}
	s := NewTaskStore(pool)
	_, err := s.ListByFilter(context.Background(), uuid.New(), model.FilterSet{})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── GetByID ──────────────────────────────────────────────────────────────────

func TestTaskStore_GetByID_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewTaskStore(pool)
	_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error")
	}
}

func TestTaskStore_GetByID_NotFound(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}
	s := NewTaskStore(pool)
	_, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ───────────────────────────────────────────────────────────────────

func TestTaskStore_Create_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewTaskStore(pool)
	_, err := s.Create(context.Background(), &model.Task{Title: "t", Status: model.StatusTodo, Priority: model.PriorityNone})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func TestTaskStore_Update_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewTaskStore(pool)
	_, err := s.Update(context.Background(), &model.Task{ID: uuid.New(), UserID: uuid.New(), Title: "t"})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Upsert ───────────────────────────────────────────────────────────────────

func TestTaskStore_Upsert_ScanError(t *testing.T) {
	pool := &mockPool{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error { return errors.New("scan error") }}
		},
	}
	s := NewTaskStore(pool)
	_, err := s.Upsert(context.Background(), &model.Task{Title: "t", Status: model.StatusTodo, Priority: model.PriorityNone})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── scanTask happy path ──────────────────────────────────────────────────────

func makeTaskScanFn(withLink bool) func(dest ...any) error {
uid := uuid.New()
id := uuid.New()
now := time.Now()
return func(dest ...any) error {
vals := []any{
id,       // ID
uid,      // UserID
"title",  // Title
"desc",   // Description
model.StatusTodo,    // Status
model.PriorityNone,  // Priority
[]string{"tag"},     // Tags
(*time.Time)(nil),   // DueDate
(*uuid.UUID)(nil),   // SourcePageID
"",                  // SourceNodeID
[]byte(nil),         // linkJSON
0.0,                 // Order
(*uuid.UUID)(nil),   // OrgID
false,               // IsPrivate
(*uuid.UUID)(nil),   // FolderID
now,                 // CreatedAt
now,                 // UpdatedAt
}
if withLink {
vals[10] = []byte(`{"url":"https://example.com","title":"Example"}`)
}
for i, d := range dest {
switch p := d.(type) {
case *uuid.UUID:
if v, ok := vals[i].(uuid.UUID); ok {
*p = v
}
case **uuid.UUID:
if v, ok := vals[i].(*uuid.UUID); ok {
*p = v
}
case *string:
if v, ok := vals[i].(string); ok {
*p = v
}
case *model.TaskStatus:
if v, ok := vals[i].(model.TaskStatus); ok {
*p = v
}
case *model.Priority:
if v, ok := vals[i].(model.Priority); ok {
*p = v
}
case *[]string:
if v, ok := vals[i].([]string); ok {
*p = v
}
case **time.Time:
if v, ok := vals[i].(*time.Time); ok {
*p = v
}
case *[]byte:
if v, ok := vals[i].([]byte); ok {
*p = v
}
case *float64:
if v, ok := vals[i].(float64); ok {
*p = v
}
case *bool:
if v, ok := vals[i].(bool); ok {
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

func TestTaskStore_GetByID_Success(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeTaskScanFn(false)}
},
}
s := NewTaskStore(pool)
task, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Errorf("unexpected error: %v", err)
}
if task == nil {
t.Error("expected non-nil task")
}
}

func TestTaskStore_GetByID_WithLink_Success(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeTaskScanFn(true)}
},
}
s := NewTaskStore(pool)
task, err := s.GetByID(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Errorf("unexpected error: %v", err)
}
if task == nil || task.Link == nil {
t.Error("expected task with link")
}
}

func TestTaskStore_ListByUserPaginated_ScanError(t *testing.T) {
callCount := 0
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
// count query returns 0
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*int) = 0
return nil
}}
},
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
callCount++
return &mockRows{
rows: []func(dest ...any) error{
func(dest ...any) error { return errors.New("scan err") },
},
}, nil
},
}
s := NewTaskStore(pool)
_, _, err := s.ListByUserPaginated(context.Background(), uuid.New(), Pagination{Limit: 10, Offset: 0})
if err == nil {
t.Error("expected error")
}
}

// ─── Task Upsert/Create/Update with non-nil Link ──────────────────────────────

func TestTaskStore_Upsert_WithLink(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeTaskScanFn(true)}
},
}
s := NewTaskStore(pool)
linkTitle := "Example"
task, err := s.Upsert(context.Background(), &model.Task{
Title:  "t",
Status: model.StatusTodo,
Link:   &model.LinkMeta{URL: "https://example.com", Title: &linkTitle},
})
if err != nil || task == nil {
t.Errorf("Upsert with link: %v", err)
}
}

func TestTaskStore_Create_WithLink(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeTaskScanFn(true)}
},
}
s := NewTaskStore(pool)
linkTitle := "Example"
task, err := s.Create(context.Background(), &model.Task{
Title:  "t",
Status: model.StatusTodo,
Link:   &model.LinkMeta{URL: "https://example.com", Title: &linkTitle},
})
if err != nil || task == nil {
t.Errorf("Create with link: %v", err)
}
}

func TestTaskStore_Update_WithLink(t *testing.T) {
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: makeTaskScanFn(true)}
},
}
s := NewTaskStore(pool)
linkTitle := "Example"
task, err := s.Update(context.Background(), &model.Task{
ID:     uuid.New(),
UserID: uuid.New(),
Title:  "t",
Link:   &model.LinkMeta{URL: "https://example.com", Title: &linkTitle},
})
if err != nil || task == nil {
t.Errorf("Update with link: %v", err)
}
}

func TestTaskStore_Delete_Success(t *testing.T) {
pool := &mockPool{
execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
return pgconn.NewCommandTag("DELETE 1"), nil
},
}
s := NewTaskStore(pool)
err := s.Delete(context.Background(), uuid.New(), uuid.New())
if err != nil {
t.Errorf("Delete success: %v", err)
}
}

// ─── Success paths ────────────────────────────────────────────────────────────

func TestTaskStore_ListByUser_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{taskRowFn(id, uid)}}, nil
},
}
s := NewTaskStore(pool)
got, err := s.ListByUser(context.Background(), uid)
if err != nil || len(got) != 1 || got[0].ID != id {
t.Fatalf("ListByUser_Success: err=%v", err)
}
}

func TestTaskStore_ListByUserPaginated_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
return &mockRow{scanFn: func(dest ...any) error {
*dest[0].(*int) = 1
return nil
}}
},
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{taskRowFn(id, uid)}}, nil
},
}
s := NewTaskStore(pool)
got, total, err := s.ListByUserPaginated(context.Background(), uid, Pagination{Limit: 10})
if err != nil || len(got) != 1 || total != 1 {
t.Fatalf("ListByUserPaginated_Success: err=%v", err)
}
}

func TestTaskStore_ListBySourcePage_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{taskRowFn(id, uid)}}, nil
},
}
s := NewTaskStore(pool)
got, err := s.ListBySourcePage(context.Background(), uuid.New(), uid)
if err != nil || len(got) != 1 {
t.Fatalf("ListBySourcePage_Success: err=%v", err)
}
}

func TestTaskStore_ListBySourceNode_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{taskRowFn(id, uid)}}, nil
},
}
s := NewTaskStore(pool)
got, err := s.ListBySourceNode(context.Background(), uid, "node-id")
if err != nil || len(got) != 1 {
t.Fatalf("ListBySourceNode_Success: err=%v", err)
}
}

func TestTaskStore_ListByFilter_Success(t *testing.T) {
id, uid := uuid.New(), uuid.New()
pool := &mockPool{
queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
return &mockRows{rows: []func(...any) error{taskRowFn(id, uid)}}, nil
},
}
s := NewTaskStore(pool)
got, err := s.ListByFilter(context.Background(), uid, model.FilterSet{Rules: []model.FilterRule{
{Field: "status", Operator: model.FilterOpEq, Value: "todo"},
}})
if err != nil || len(got) != 1 {
t.Fatalf("ListByFilter_Success: err=%v", err)
}
}
