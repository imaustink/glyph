package memstore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

var ctx = context.Background()

// ─── NewStores ────────────────────────────────────────────────────────────────

func TestNewStores_ReturnsAllNonNil(t *testing.T) {
	us, ps, ts, ls, tmpl, orgs, sh := NewStores()
	if us == nil || ps == nil || ts == nil || ls == nil || tmpl == nil || orgs == nil || sh == nil {
		t.Error("NewStores returned one or more nil stores")
	}
}

// ─── Registry.Reset ──────────────────────────────────────────────────────────

func TestRegistry_Reset_ClearsData(t *testing.T) {
	r := NewRegistry()
	id := uuid.New()
	r.usersByID[id] = &model.User{ID: id}
	r.Reset()
	if len(r.usersByID) != 0 {
		t.Error("Reset should clear usersByID")
	}
}

// ─── userStore ───────────────────────────────────────────────────────────────

func TestUserStore_GetByID_Found(t *testing.T) {
	s := &userStore{r: NewRegistry()}
	email := "u@example.com"
	u, _ := s.Upsert(ctx, "sub", "iss", &email, nil)
	got, err := s.GetByID(ctx, u.ID)
	if err != nil || got.ID != u.ID {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestUserStore_GetByID_NotFound(t *testing.T) {
	s := &userStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New()); err == nil {
		t.Error("expected error for unknown user")
	}
}

func TestUserStore_GetByEmail_Found(t *testing.T) {
	s := &userStore{r: NewRegistry()}
	email := "found@example.com"
	u, _ := s.Upsert(ctx, "sub", "iss", &email, nil)
	got, err := s.GetByEmail(ctx, "found@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("GetByEmail: %v", err)
	}
}

func TestUserStore_GetByEmail_NotFound(t *testing.T) {
	s := &userStore{r: NewRegistry()}
	if _, err := s.GetByEmail(ctx, "nobody@example.com"); err == nil {
		t.Error("expected error for unknown email")
	}
}

func TestUserStore_Reset(t *testing.T) {
	s := &userStore{r: NewRegistry()}
	email := "x@x.com"
	s.Upsert(ctx, "sub", "iss", &email, nil) //nolint:errcheck
	s.Reset()
	if _, err := s.GetByEmail(ctx, "x@x.com"); err == nil {
		t.Error("expected error after Reset")
	}
}

// ─── pageStore ───────────────────────────────────────────────────────────────

func TestPageStore_Reset(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "T"})
	s.Reset()
	if _, err := s.GetByID(ctx, p.ID, userID); err == nil {
		t.Error("expected error after Reset")
	}
}

func TestPageStore_ListByUser_Empty(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	pages, err := s.ListByUser(ctx, uuid.New())
	if err != nil || len(pages) != 0 {
		t.Errorf("expected empty: %v %v", pages, err)
	}
}

func TestPageStore_ListByUser_WithData(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "A"}) //nolint:errcheck
	s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "B"}) //nolint:errcheck
	pages, err := s.ListByUser(ctx, userID)
	if err != nil || len(pages) != 2 {
		t.Errorf("expected 2 pages: %v", err)
	}
}

func TestPageStore_GetByID_NotFound(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for unknown page")
	}
}

func TestPageStore_GetByID_Found(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "X"})
	got, err := s.GetByID(ctx, p.ID, userID)
	if err != nil || got.ID != p.ID {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestPageStore_Update_Success(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "Old"})
	p.Title = "New"
	updated, err := s.Update(ctx, p)
	if err != nil || updated.Title != "New" {
		t.Fatalf("Update: %v", err)
	}
}

func TestPageStore_Update_NotFound(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	_, err := s.Update(ctx, &model.Page{ID: uuid.New(), UserID: uuid.New(), Type: model.NodeTypePage})
	if err == nil {
		t.Error("expected error for unknown page")
	}
}

func TestPageStore_Delete_Success(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "Del"})
	s.Delete(ctx, p.ID, userID) //nolint:errcheck
	if _, err := s.GetByID(ctx, p.ID, userID); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestPageStore_Delete_WrongUser_NoError(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "Del"})
	if err := s.Delete(ctx, p.ID, uuid.New()); err != nil {
		t.Errorf("Delete wrong user should not error: %v", err)
	}
	// Page should still exist
	if _, err := s.GetByID(ctx, p.ID, userID); err != nil {
		t.Error("page should still exist after wrong-user delete")
	}
}

func TestPageStore_Delete_AlsoDeletesContent(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "T"})
	s.UpsertContent(ctx, &model.PageContent{PageID: p.ID, Content: json.RawMessage(`{}`)}, userID) //nolint:errcheck
	s.Delete(ctx, p.ID, userID)                                                                    //nolint:errcheck
	if _, err := s.GetContent(ctx, p.ID, userID); err == nil {
		t.Error("expected error for content after page delete")
	}
}

func TestPageStore_GetContent_PageNotFound(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	if _, err := s.GetContent(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for missing page")
	}
}

func TestPageStore_GetContent_ContentNotFound(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "T"})
	if _, err := s.GetContent(ctx, p.ID, userID); err == nil {
		t.Error("expected error when content not stored yet")
	}
}

func TestPageStore_GetContent_Success(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "T"})
	s.UpsertContent(ctx, &model.PageContent{PageID: p.ID, Content: json.RawMessage(`{"type":"doc"}`)}, userID) //nolint:errcheck
	pc, err := s.GetContent(ctx, p.ID, userID)
	if err != nil || pc == nil {
		t.Fatalf("GetContent: %v", err)
	}
}

// ─── taskStore ───────────────────────────────────────────────────────────────

func TestTaskStore_Reset(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	task, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "T"})
	s.Reset()
	if _, err := s.GetByID(ctx, task.ID, userID); err == nil {
		t.Error("expected error after Reset")
	}
}

func TestTaskStore_ListByUser(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Task{UserID: userID, Title: "A"}) //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "B"}) //nolint:errcheck
	tasks, err := s.ListByUser(ctx, userID)
	if err != nil || len(tasks) != 2 {
		t.Errorf("expected 2 tasks: %v", err)
	}
}

func TestTaskStore_ListByUserPaginated(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	for i := 0; i < 5; i++ {
		s.Create(ctx, &model.Task{UserID: userID, Title: "T"}) //nolint:errcheck
	}
	tasks, total, err := s.ListByUserPaginated(ctx, userID, store.Pagination{Offset: 1, Limit: 3})
	if err != nil || total != 5 || len(tasks) != 3 {
		t.Errorf("paginated: total=%d len=%d err=%v", total, len(tasks), err)
	}
}

func TestTaskStore_GetByID_NotFound(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestTaskStore_GetByID_Found(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	task, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "T"})
	got, err := s.GetByID(ctx, task.ID, userID)
	if err != nil || got.ID != task.ID {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestTaskStore_ListBySourcePage(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	pageID := uuid.New()
	s.Create(ctx, &model.Task{UserID: userID, Title: "T", SourcePageID: &pageID}) //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "Other"})                    //nolint:errcheck
	tasks, err := s.ListBySourcePage(ctx, userID, pageID)
	if err != nil || len(tasks) != 1 {
		t.Errorf("expected 1 task: %d %v", len(tasks), err)
	}
}

func TestTaskStore_Upsert_NewTask(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	task, err := s.Upsert(ctx, &model.Task{UserID: userID, Title: "New"})
	if err != nil || task.ID == uuid.Nil {
		t.Fatalf("Upsert new: %v", err)
	}
}

func TestTaskStore_Upsert_UpdateExisting(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	t1, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "Old"})
	t1.Title = "New"
	updated, err := s.Upsert(ctx, t1)
	if err != nil || updated.Title != "New" {
		t.Fatalf("Upsert update: %v", err)
	}
}

func TestTaskStore_Upsert_WrongUser(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	t1, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "T"})
	t1.UserID = uuid.New()
	if _, err := s.Upsert(ctx, t1); err == nil {
		t.Error("expected error when upserting with wrong userID")
	}
}

func TestTaskStore_Update_Success(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	task, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "Old"})
	task.Title = "New"
	updated, err := s.Update(ctx, task)
	if err != nil || updated.Title != "New" {
		t.Fatalf("Update: %v", err)
	}
}

func TestTaskStore_Update_NotFound(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	_, err := s.Update(ctx, &model.Task{ID: uuid.New(), UserID: uuid.New(), Title: "T"})
	if err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestTaskStore_Delete_Success(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	task, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "T"})
	s.Delete(ctx, task.ID, userID) //nolint:errcheck
	if _, err := s.GetByID(ctx, task.ID, userID); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestTaskStore_Delete_WrongUser(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	task, _ := s.Create(ctx, &model.Task{UserID: userID, Title: "T"})
	s.Delete(ctx, task.ID, uuid.New()) //nolint:errcheck
	if _, err := s.GetByID(ctx, task.ID, userID); err != nil {
		t.Error("task should still exist after wrong-user delete")
	}
}

func TestTaskStore_ListByFilter_NoRules(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Task{UserID: userID, Title: "T"}) //nolint:errcheck
	tasks, err := s.ListByFilter(ctx, userID, model.FilterSet{})
	if err != nil || len(tasks) != 1 {
		t.Errorf("expected 1 task with no-rule filter: %v", err)
	}
}

func TestTaskStore_ListByFilter_AndConjunction(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Task{UserID: userID, Title: "Alpha", Status: model.StatusTodo}) //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "Beta", Status: model.StatusDone})  //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "Gamma", Status: model.StatusTodo}) //nolint:errcheck

	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpEq, Value: "todo"},
			{Field: "title", Operator: model.FilterOpEq, Value: "Alpha"},
		},
	}
	tasks, err := s.ListByFilter(ctx, userID, fs)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "Alpha" {
		t.Errorf("AND filter: expected 1 (Alpha), got %d: %v", len(tasks), err)
	}
}

func TestTaskStore_ListByFilter_OrConjunction(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Task{UserID: userID, Title: "Alpha", Status: model.StatusTodo})       //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "Beta", Status: model.StatusDone})        //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "Gamma", Status: model.StatusInProgress}) //nolint:errcheck

	fs := model.FilterSet{
		Conjunction: model.ConjunctionOr,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpEq, Value: "todo"},
			{Field: "status", Operator: model.FilterOpEq, Value: "done"},
		},
	}
	tasks, err := s.ListByFilter(ctx, userID, fs)
	if err != nil || len(tasks) != 2 {
		t.Errorf("OR filter: expected 2, got %d: %v", len(tasks), err)
	}
}

// ─── memstoreMatchesRule — all operators ─────────────────────────────────────

func makeTask(status model.TaskStatus, title string, tags []string, dueDate *string) *model.Task {
	return &model.Task{Status: status, Title: title, Tags: tags, DueDate: dueDate}
}

func TestTaskStore_ListByFilter_SourcePageTags(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	ps := &pageStore{r: s.r}
	userID := uuid.New()

	workPage, _ := ps.Create(ctx, &model.Page{UserID: userID, Type: "page", Title: "Work", Tags: []string{"work", "urgent"}})
	homePage, _ := ps.Create(ctx, &model.Page{UserID: userID, Type: "page", Title: "Home", Tags: []string{"home"}})

	s.Create(ctx, &model.Task{UserID: userID, Title: "FromWork", SourcePageID: &workPage.ID}) //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "FromHome", SourcePageID: &homePage.ID}) //nolint:errcheck
	s.Create(ctx, &model.Task{UserID: userID, Title: "Standalone"})                            //nolint:errcheck

	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "sourcePageTags", Operator: model.FilterOpContains, Value: "work"},
		},
	}
	tasks, err := s.ListByFilter(ctx, userID, fs)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "FromWork" {
		t.Fatalf("sourcePageTags contains: expected 1 (FromWork), got %d: %v", len(tasks), err)
	}
}

func TestMemstoreMatchesTagRule(t *testing.T) {
	tags := []string{"Work", "urgent"}
	cases := []struct {
		op   model.FilterOperator
		val  interface{}
		want bool
	}{
		{model.FilterOpAny, nil, true},
		{model.FilterOpContains, "work", true}, // case-insensitive
		{model.FilterOpContains, "home", false},
		{model.FilterOpEq, "urgent", true},
		{model.FilterOpNeq, "home", true},
		{model.FilterOpNeq, "work", false},
		{model.FilterOpIn, []string{"home", "urgent"}, true},
		{model.FilterOpIn, []string{"home"}, false},
		{model.FilterOpNotIn, []string{"home"}, true},
		{model.FilterOpNotIn, []string{"work"}, false},
		{model.FilterOpExists, nil, true},
		{model.FilterOpNotExists, nil, false},
	}
	for _, c := range cases {
		got := memstoreMatchesTagRule(tags, model.FilterRule{Operator: c.op, Value: c.val})
		if got != c.want {
			t.Errorf("op %s val %v: want %v got %v", c.op, c.val, c.want, got)
		}
	}
	// Empty tags: exists false, not_exists true.
	if memstoreMatchesTagRule(nil, model.FilterRule{Operator: model.FilterOpExists}) {
		t.Error("exists on empty tags should be false")
	}
	if !memstoreMatchesTagRule(nil, model.FilterRule{Operator: model.FilterOpNotExists}) {
		t.Error("not_exists on empty tags should be true")
	}
}

func TestMatchesRule_FilterOpAny(t *testing.T) {
	if !memstoreMatchesRule(makeTask(model.StatusTodo, "T", nil, nil), model.FilterRule{Operator: model.FilterOpAny}) {
		t.Error("FilterOpAny should always match")
	}
}

func TestMatchesRule_FilterOpEq(t *testing.T) {
	task := makeTask(model.StatusTodo, "Hello", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpEq, Value: "todo"}) {
		t.Error("Eq match failed")
	}
	if memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpEq, Value: "done"}) {
		t.Error("Eq should not match wrong value")
	}
}

func TestMatchesRule_FilterOpNeq(t *testing.T) {
	task := makeTask(model.StatusTodo, "Hello", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpNeq, Value: "done"}) {
		t.Error("Neq match failed")
	}
}

func TestMatchesRule_FilterOpIn(t *testing.T) {
	task := makeTask(model.StatusTodo, "Hello", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpIn, Value: []string{"todo", "done"}}) {
		t.Error("In match failed")
	}
	if memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpIn, Value: []string{"done"}}) {
		t.Error("In should not match when value not in list")
	}
}

func TestMatchesRule_FilterOpNotIn(t *testing.T) {
	task := makeTask(model.StatusTodo, "Hello", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpNotIn, Value: []string{"done"}}) {
		t.Error("NotIn match failed")
	}
	if memstoreMatchesRule(task, model.FilterRule{Field: "status", Operator: model.FilterOpNotIn, Value: []string{"todo"}}) {
		t.Error("NotIn should not match when value is in list")
	}
}

func TestMatchesRule_FilterOpContains_Tags(t *testing.T) {
	task := makeTask(model.StatusTodo, "Hello", []string{"go", "test"}, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "tags", Operator: model.FilterOpContains, Value: "go"}) {
		t.Error("Contains tags match failed")
	}
	if memstoreMatchesRule(task, model.FilterRule{Field: "tags", Operator: model.FilterOpContains, Value: "java"}) {
		t.Error("Contains tags should not match absent tag")
	}
}

func TestMatchesRule_FilterOpContains_Title(t *testing.T) {
	task := makeTask(model.StatusTodo, "Hello World", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "title", Operator: model.FilterOpContains, Value: "hello"}) {
		t.Error("Contains title case-insensitive match failed")
	}
}

func TestMatchesRule_FilterOpExists(t *testing.T) {
	due := "2024-12-31"
	task := makeTask(model.StatusTodo, "T", nil, &due)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "dueDate", Operator: model.FilterOpExists}) {
		t.Error("Exists should match when field is set")
	}
	task2 := makeTask(model.StatusTodo, "T", nil, nil)
	if memstoreMatchesRule(task2, model.FilterRule{Field: "dueDate", Operator: model.FilterOpExists}) {
		t.Error("Exists should not match when field is nil")
	}
}

func TestMatchesRule_FilterOpNotExists(t *testing.T) {
	task := makeTask(model.StatusTodo, "T", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Field: "dueDate", Operator: model.FilterOpNotExists}) {
		t.Error("NotExists should match when field is nil")
	}
	due := "2024-12-31"
	task2 := makeTask(model.StatusTodo, "T", nil, &due)
	if memstoreMatchesRule(task2, model.FilterRule{Field: "dueDate", Operator: model.FilterOpNotExists}) {
		t.Error("NotExists should not match when field is set")
	}
}

func TestMatchesRule_DefaultOperator_ReturnsTrue(t *testing.T) {
	task := makeTask(model.StatusTodo, "T", nil, nil)
	if !memstoreMatchesRule(task, model.FilterRule{Operator: "unknown_op"}) {
		t.Error("default operator should return true")
	}
}

// ─── memstoreField — all branches ────────────────────────────────────────────

func TestMemstoreField_Status(t *testing.T) {
	task := &model.Task{Status: model.StatusDone}
	if v := memstoreField(task, "status"); v != "done" {
		t.Errorf("status field: got %v", v)
	}
}

func TestMemstoreField_Priority(t *testing.T) {
	task := &model.Task{Priority: model.PriorityHigh}
	if v := memstoreField(task, "priority"); v != "high" {
		t.Errorf("priority field: got %v", v)
	}
}

func TestMemstoreField_Title(t *testing.T) {
	task := &model.Task{Title: "My Task"}
	if v := memstoreField(task, "title"); v != "My Task" {
		t.Errorf("title field: got %v", v)
	}
}

func TestMemstoreField_DueDate_NonNil(t *testing.T) {
	due := "2024-12-31"
	task := &model.Task{DueDate: &due}
	if v := memstoreField(task, "dueDate"); v != "2024-12-31" {
		t.Errorf("dueDate field: got %v", v)
	}
}

func TestMemstoreField_SourcePageID_NonNil(t *testing.T) {
	id := uuid.New()
	task := &model.Task{SourcePageID: &id}
	if v := memstoreField(task, "sourcePageId"); v == nil {
		t.Error("sourcePageId field: expected non-nil")
	}
}

func TestMemstoreField_Tags(t *testing.T) {
	task := &model.Task{Tags: []string{"a", "b"}}
	v := memstoreField(task, "tags")
	if v == nil {
		t.Error("tags field: expected non-nil")
	}
}

// ─── laneStore ───────────────────────────────────────────────────────────────

func TestLaneStore_Reset(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	lane, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "T"})
	s.Reset()
	if _, err := s.GetByID(ctx, lane.ID, userID); err == nil {
		t.Error("expected error after Reset")
	}
}

func TestLaneStore_ListByUser(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Lane{UserID: userID, Title: "A"}) //nolint:errcheck
	s.Create(ctx, &model.Lane{UserID: userID, Title: "B"}) //nolint:errcheck
	lanes, err := s.ListByUser(ctx, userID)
	if err != nil || len(lanes) != 2 {
		t.Errorf("expected 2 lanes: %v", err)
	}
}

func TestLaneStore_GetByID_NotFound(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for unknown lane")
	}
}

func TestLaneStore_GetByID_Found(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	lane, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "T"})
	got, err := s.GetByID(ctx, lane.ID, userID)
	if err != nil || got.ID != lane.ID {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestLaneStore_Upsert_New(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	lane, err := s.Upsert(ctx, &model.Lane{UserID: userID, Title: "New"})
	if err != nil || lane.ID == uuid.Nil {
		t.Fatalf("Upsert new: %v", err)
	}
}

func TestLaneStore_Upsert_UpdateExisting(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	l, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "Old"})
	l.Title = "Updated"
	updated, err := s.Upsert(ctx, l)
	if err != nil || updated.Title != "Updated" {
		t.Fatalf("Upsert update: %v", err)
	}
}

func TestLaneStore_Upsert_WrongUser(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	l, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "T"})
	l.UserID = uuid.New()
	if _, err := s.Upsert(ctx, l); err == nil {
		t.Error("expected error upserting with wrong userID")
	}
}

func TestLaneStore_ReorderAll(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	l1, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "A", Order: 1})
	l2, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "B", Order: 2})
	err := s.ReorderAll(ctx, userID, []store.LaneReorderItem{{ID: l1.ID, Order: 10}, {ID: l2.ID, Order: 20}})
	if err != nil {
		t.Fatalf("ReorderAll: %v", err)
	}
	got1, _ := s.GetByID(ctx, l1.ID, userID)
	if got1.Order != 10 {
		t.Errorf("expected order 10, got %d", got1.Order)
	}
}

func TestLaneStore_BatchCreate(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	lanes, err := s.BatchCreate(ctx, []*model.Lane{
		{UserID: userID, Title: "A"},
		{UserID: userID, Title: "B"},
	})
	if err != nil || len(lanes) != 2 {
		t.Fatalf("BatchCreate: %v", err)
	}
}

func TestLaneStore_Update_Success(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	l, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "Old"})
	l.Title = "New"
	updated, err := s.Update(ctx, l)
	if err != nil || updated.Title != "New" {
		t.Fatalf("Update: %v", err)
	}
}

func TestLaneStore_Update_NotFound(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	_, err := s.Update(ctx, &model.Lane{ID: uuid.New(), UserID: uuid.New(), Title: "T"})
	if err == nil {
		t.Error("expected error for unknown lane")
	}
}

func TestLaneStore_Delete_Success(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	l, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "T"})
	s.Delete(ctx, l.ID, userID) //nolint:errcheck
	if _, err := s.GetByID(ctx, l.ID, userID); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestLaneStore_Delete_WrongUser(t *testing.T) {
	s := &laneStore{r: NewRegistry()}
	userID := uuid.New()
	l, _ := s.Create(ctx, &model.Lane{UserID: userID, Title: "T"})
	s.Delete(ctx, l.ID, uuid.New()) //nolint:errcheck
	if _, err := s.GetByID(ctx, l.ID, userID); err != nil {
		t.Error("lane should still exist after wrong-user delete")
	}
}

// ─── templateStore ───────────────────────────────────────────────────────────

func TestTemplateStore_Reset(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "T"})
	s.Reset()
	if _, err := s.GetByID(ctx, tmpl.ID, userID); err == nil {
		t.Error("expected error after Reset")
	}
}

func TestTemplateStore_ListByUser(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Template{UserID: userID, Name: "A"}) //nolint:errcheck
	s.Create(ctx, &model.Template{UserID: userID, Name: "B"}) //nolint:errcheck
	templates, err := s.ListByUser(ctx, userID)
	if err != nil || len(templates) != 2 {
		t.Errorf("expected 2 templates: %v", err)
	}
}

func TestTemplateStore_GetByID_NotFound(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for unknown template")
	}
}

func TestTemplateStore_GetByID_Found(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "T"})
	got, err := s.GetByID(ctx, tmpl.ID, userID)
	if err != nil || got.ID != tmpl.ID {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestTemplateStore_Upsert_New(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, err := s.Upsert(ctx, &model.Template{UserID: userID, Name: "New"})
	if err != nil || tmpl.ID == uuid.Nil {
		t.Fatalf("Upsert new: %v", err)
	}
}

func TestTemplateStore_Upsert_UpdateExisting(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "Old"})
	tmpl.Name = "Updated"
	updated, err := s.Upsert(ctx, tmpl)
	if err != nil || updated.Name != "Updated" {
		t.Fatalf("Upsert update: %v", err)
	}
}

func TestTemplateStore_Upsert_WrongUser(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "T"})
	tmpl.UserID = uuid.New()
	if _, err := s.Upsert(ctx, tmpl); err == nil {
		t.Error("expected error when upserting with wrong userID")
	}
}

func TestTemplateStore_Update_Success(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "Old"})
	tmpl.Name = "New"
	updated, err := s.Update(ctx, tmpl)
	if err != nil || updated.Name != "New" {
		t.Fatalf("Update: %v", err)
	}
}

func TestTemplateStore_Update_NotFound(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	_, err := s.Update(ctx, &model.Template{ID: uuid.New(), UserID: uuid.New(), Name: "T"})
	if err == nil {
		t.Error("expected error for unknown template")
	}
}

func TestTemplateStore_Delete_Success(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "T"})
	s.Delete(ctx, tmpl.ID, userID) //nolint:errcheck
	if _, err := s.GetByID(ctx, tmpl.ID, userID); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestTemplateStore_Delete_WrongUser(t *testing.T) {
	s := &templateStore{r: NewRegistry()}
	userID := uuid.New()
	tmpl, _ := s.Create(ctx, &model.Template{UserID: userID, Name: "T"})
	s.Delete(ctx, tmpl.ID, uuid.New()) //nolint:errcheck
	if _, err := s.GetByID(ctx, tmpl.ID, userID); err != nil {
		t.Error("template should still exist after wrong-user delete")
	}
}

// ─── orgStore ────────────────────────────────────────────────────────────────

func TestOrgStore_Create_Success(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	org, err := s.Create(ctx, &model.Organization{Name: "TestOrg"})
	if err != nil || org.ID == uuid.Nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestOrgStore_GetByID_NotFound(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New()); err == nil {
		t.Error("expected error for unknown org")
	}
}

func TestOrgStore_GetByID_CountsMembers(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	org, _ := s.Create(ctx, &model.Organization{Name: "Org"})
	r.members[orgMemberKey{org.ID, uuid.New()}] = &model.OrgMember{}
	r.members[orgMemberKey{org.ID, uuid.New()}] = &model.OrgMember{}
	got, err := s.GetByID(ctx, org.ID)
	if err != nil || got.MemberCount != 2 {
		t.Errorf("GetByID member count: %d %v", got.MemberCount, err)
	}
}

func TestOrgStore_ListForUser(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	userID := uuid.New()
	org, _ := s.Create(ctx, &model.Organization{Name: "MyOrg"})
	r.members[orgMemberKey{org.ID, userID}] = &model.OrgMember{OrgID: org.ID, UserID: userID, Role: model.OrgRoleOwner}
	orgs, err := s.ListForUser(ctx, userID)
	if err != nil || len(orgs) != 1 {
		t.Errorf("expected 1 org: %v", err)
	}
}

func TestOrgStore_Update_Success(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	org, _ := s.Create(ctx, &model.Organization{Name: "Old"})
	org.Name = "New"
	updated, err := s.Update(ctx, org)
	if err != nil || updated.Name != "New" {
		t.Fatalf("Update: %v", err)
	}
}

func TestOrgStore_Update_NotFound(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	_, err := s.Update(ctx, &model.Organization{ID: uuid.New(), Name: "T"})
	if err == nil {
		t.Error("expected error for unknown org")
	}
}

func TestOrgStore_Delete_RemovesOrgAndMembers(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	org, _ := s.Create(ctx, &model.Organization{Name: "Org"})
	memberID := uuid.New()
	r.members[orgMemberKey{org.ID, memberID}] = &model.OrgMember{}
	s.Delete(ctx, org.ID) //nolint:errcheck
	if _, err := s.GetByID(ctx, org.ID); err == nil {
		t.Error("org should be deleted")
	}
	if _, ok := r.members[orgMemberKey{org.ID, memberID}]; ok {
		t.Error("member should be removed when org is deleted")
	}
}

func TestOrgStore_AddMember(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	orgID := uuid.New()
	userID := uuid.New()
	m, err := s.AddMember(ctx, orgID, userID, model.OrgRoleViewer)
	if err != nil || m.UserID != userID {
		t.Fatalf("AddMember: %v", err)
	}
}

func TestOrgStore_AddMember_WithUserEmail(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	email := "member@example.com"
	u, _ := (&userStore{r: r}).Upsert(ctx, "sub", "iss", &email, nil)
	orgID := uuid.New()
	m, err := s.AddMember(ctx, orgID, u.ID, model.OrgRoleViewer)
	if err != nil || m.Email == nil || *m.Email != email {
		t.Fatalf("AddMember with user: email=%v err=%v", m.Email, err)
	}
}

func TestOrgStore_GetMember_NotFound(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	if _, err := s.GetMember(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("expected error for unknown member")
	}
}

func TestOrgStore_GetMember_Found(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	orgID := uuid.New()
	userID := uuid.New()
	s.AddMember(ctx, orgID, userID, model.OrgRoleViewer) //nolint:errcheck
	m, err := s.GetMember(ctx, orgID, userID)
	if err != nil || m.UserID != userID {
		t.Fatalf("GetMember: %v", err)
	}
}

func TestOrgStore_ListMembers(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	orgID := uuid.New()
	s.AddMember(ctx, orgID, uuid.New(), model.OrgRoleViewer) //nolint:errcheck
	s.AddMember(ctx, orgID, uuid.New(), model.OrgRoleOwner)  //nolint:errcheck
	members, err := s.ListMembers(ctx, orgID)
	if err != nil || len(members) != 2 {
		t.Errorf("expected 2 members: %v", err)
	}
}

func TestOrgStore_UpdateMemberRole_Success(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	orgID := uuid.New()
	userID := uuid.New()
	s.AddMember(ctx, orgID, userID, model.OrgRoleViewer) //nolint:errcheck
	m, err := s.UpdateMemberRole(ctx, orgID, userID, model.OrgRoleOwner)
	if err != nil || m.Role != model.OrgRoleOwner {
		t.Fatalf("UpdateMemberRole: %v", err)
	}
}

func TestOrgStore_UpdateMemberRole_NotFound(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	_, err := s.UpdateMemberRole(ctx, uuid.New(), uuid.New(), model.OrgRoleOwner)
	if err == nil {
		t.Error("expected error for unknown member")
	}
}

func TestOrgStore_RemoveMember(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	orgID := uuid.New()
	userID := uuid.New()
	s.AddMember(ctx, orgID, userID, model.OrgRoleViewer) //nolint:errcheck
	s.RemoveMember(ctx, orgID, userID)                   //nolint:errcheck
	if _, err := s.GetMember(ctx, orgID, userID); err == nil {
		t.Error("member should be removed")
	}
}

func TestOrgStore_GetUserOrgIDs(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	userID := uuid.New()
	orgID1, orgID2 := uuid.New(), uuid.New()
	s.AddMember(ctx, orgID1, userID, model.OrgRoleViewer) //nolint:errcheck
	s.AddMember(ctx, orgID2, userID, model.OrgRoleOwner)  //nolint:errcheck
	ids, err := s.GetUserOrgIDs(ctx, userID)
	if err != nil || len(ids) != 2 {
		t.Errorf("expected 2 org IDs: %v", err)
	}
}

func TestOrgStore_Reset(t *testing.T) {
	s := &orgStore{r: NewRegistry()}
	org, _ := s.Create(ctx, &model.Organization{Name: "Org"})
	s.Reset()
	if _, err := s.GetByID(ctx, org.ID); err == nil {
		t.Error("expected error after Reset")
	}
}

// ─── shareStore ──────────────────────────────────────────────────────────────

func TestShareStore_Create_Success(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	sh, err := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedWith:   model.ShareUser{ID: uuid.New()},
		Permission:   model.SharePermissionViewer,
	})
	if err != nil || sh.ID == uuid.Nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestShareStore_Create_HydratesUserEmail(t *testing.T) {
	r := NewRegistry()
	s := &shareStore{r: r}
	email := "sharee@example.com"
	u, _ := (&userStore{r: r}).Upsert(ctx, "sub", "iss", &email, nil)
	sh, err := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedWith:   model.ShareUser{ID: u.ID},
		Permission:   model.SharePermissionViewer,
	})
	if err != nil || sh.SharedWith.Email == nil || *sh.SharedWith.Email != email {
		t.Fatalf("Create email hydration: %v", err)
	}
}

func TestShareStore_GetByID_NotFound(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	if _, err := s.GetByID(ctx, uuid.New()); err == nil {
		t.Error("expected error for unknown share")
	}
}

func TestShareStore_GetByID_Found(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	sh, _ := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedWith:   model.ShareUser{ID: uuid.New()},
	})
	got, err := s.GetByID(ctx, sh.ID)
	if err != nil || got.ID != sh.ID {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestShareStore_ListForResource(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	resourceID := uuid.New()
	s.Create(ctx, &model.Share{ResourceType: model.ShareResourcePage, ResourceID: resourceID, SharedWith: model.ShareUser{ID: uuid.New()}}) //nolint:errcheck
	s.Create(ctx, &model.Share{ResourceType: model.ShareResourcePage, ResourceID: resourceID, SharedWith: model.ShareUser{ID: uuid.New()}}) //nolint:errcheck
	s.Create(ctx, &model.Share{ResourceType: model.ShareResourcePage, ResourceID: uuid.New(), SharedWith: model.ShareUser{ID: uuid.New()}}) //nolint:errcheck
	shares, err := s.ListForResource(ctx, model.ShareResourcePage, resourceID)
	if err != nil || len(shares) != 2 {
		t.Errorf("expected 2 shares: %d %v", len(shares), err)
	}
}

func TestShareStore_GetForUserAndResource_Found(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	userID := uuid.New()
	resourceID := uuid.New()
	if _, err := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   resourceID,
		SharedWith:   model.ShareUser{ID: userID},
		Permission:   model.SharePermissionViewer,
	}); err != nil {
		t.Fatalf("Create share: %v", err)
	}
	sh, err := s.GetForUserAndResource(ctx, userID, model.ShareResourcePage, resourceID)
	if err != nil || sh.SharedWith.ID != userID {
		t.Fatalf("GetForUserAndResource: %v", err)
	}
}

func TestShareStore_GetForUserAndResource_NotFound(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	if _, err := s.GetForUserAndResource(ctx, uuid.New(), model.ShareResourcePage, uuid.New()); err == nil {
		t.Error("expected error when not found")
	}
}

func TestShareStore_UpdatePermission_Success(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	sh, _ := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedWith:   model.ShareUser{ID: uuid.New()},
		Permission:   model.SharePermissionViewer,
	})
	updated, err := s.UpdatePermission(ctx, sh.ID, model.SharePermissionEditor)
	if err != nil || updated.Permission != model.SharePermissionEditor {
		t.Fatalf("UpdatePermission: %v", err)
	}
}

func TestShareStore_UpdatePermission_NotFound(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	if _, err := s.UpdatePermission(ctx, uuid.New(), model.SharePermissionEditor); err == nil {
		t.Error("expected error for unknown share")
	}
}

func TestShareStore_Delete_Success(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	sh, _ := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedWith:   model.ShareUser{ID: uuid.New()},
	})
	s.Delete(ctx, sh.ID) //nolint:errcheck
	if _, err := s.GetByID(ctx, sh.ID); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestShareStore_Reset(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	sh, _ := s.Create(ctx, &model.Share{
		ResourceType: model.ShareResourcePage,
		ResourceID:   uuid.New(),
		SharedWith:   model.ShareUser{ID: uuid.New()},
	})
	s.Reset()
	if _, err := s.GetByID(ctx, sh.ID); err == nil {
		t.Error("expected error after Reset")
	}
}

// ─── clone helpers ─────────────────────────────────────────────────────────

func TestCloneLane_WithAllOptionalFields(t *testing.T) {
	f := "priority"
	d := model.SortDirectionAsc
	l := &model.Lane{
		ID:    uuid.New(),
		Title: "Lane",
		FilterSet: model.FilterSet{
			Conjunction: model.ConjunctionAnd,
			Rules:       []model.FilterRule{{Field: "status", Operator: model.FilterOpEq, Value: "todo"}},
		},
		SortConfig: model.SortConfig{
			Field:     &f,
			Direction: &d,
			TaskOrder: []string{"a", "b"},
		},
	}
	cp := cloneLane(l)
	if cp.SortConfig.Field == l.SortConfig.Field {
		t.Error("cloneLane should deep-copy SortConfig.Field pointer")
	}
	if len(cp.FilterSet.Rules) != 1 {
		t.Error("cloneLane should copy filter rules")
	}
}

func TestCloneTemplate_WithOptionalFields(t *testing.T) {
	orgID := uuid.New()
	tmpl := &model.Template{
		ID:    uuid.New(),
		Name:  "T",
		OrgID: &orgID,
		TodoTrigger: &model.TodoTriggerConfig{
			Pattern:    "TODO",
			BlockTypes: []string{"paragraph"},
		},
	}
	cp := cloneTemplate(tmpl)
	if cp.OrgID == tmpl.OrgID {
		t.Error("cloneTemplate should deep-copy OrgID pointer")
	}
	if len(cp.TodoTrigger.BlockTypes) != 1 {
		t.Error("cloneTemplate should copy TodoTrigger.BlockTypes")
	}
}

func TestCloneOrg(t *testing.T) {
	org := &model.Organization{ID: uuid.New(), Name: "Org"}
	cp := cloneOrg(org)
	cp.Name = "Other"
	if org.Name == "Other" {
		t.Error("cloneOrg should return independent copy")
	}
}

func TestCloneMember(t *testing.T) {
	m := &model.OrgMember{OrgID: uuid.New(), UserID: uuid.New(), Role: model.OrgRoleViewer}
	cp := cloneMember(m)
	cp.Role = model.OrgRoleOwner
	if m.Role == model.OrgRoleOwner {
		t.Error("cloneMember should return independent copy")
	}
}

func TestCloneShare(t *testing.T) {
	sh := &model.Share{ID: uuid.New(), Permission: model.SharePermissionViewer}
	cp := cloneShare(sh)
	cp.Permission = model.SharePermissionEditor
	if sh.Permission == model.SharePermissionEditor {
		t.Error("cloneShare should return independent copy")
	}
}

// ─── Additional coverage for low% functions ──────────────────────────────────

func TestClonePage_WithAllOptionalFields(t *testing.T) {
	orgID := uuid.New()
	parentID := uuid.New()
	p := &model.Page{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Type:     model.NodeTypePage,
		Title:    "P",
		Tags:     []string{"a", "b"},
		ParentID: &parentID,
		OrgID:    &orgID,
		TodoTrigger: &model.TodoTriggerConfig{
			Pattern:    "TODO",
			BlockTypes: []string{"paragraph"},
		},
	}
	cp := clonePage(p)
	// Verify deep copies
	if cp.Tags == nil || &cp.Tags[0] == &p.Tags[0] {
		t.Error("clonePage should deep-copy Tags")
	}
	if cp.ParentID == p.ParentID {
		t.Error("clonePage should deep-copy ParentID pointer")
	}
	if cp.OrgID == p.OrgID {
		t.Error("clonePage should deep-copy OrgID pointer")
	}
	if cp.TodoTrigger == p.TodoTrigger {
		t.Error("clonePage should deep-copy TodoTrigger pointer")
	}
	if len(cp.TodoTrigger.BlockTypes) != 1 {
		t.Error("clonePage should deep-copy TodoTrigger.BlockTypes")
	}
}

func TestPageStore_Upsert_NewPage(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	p, err := s.Upsert(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "New"})
	if err != nil || p.ID == uuid.Nil {
		t.Fatalf("Upsert new page: %v", err)
	}
}

func TestPageStore_Upsert_UpdateExistingPage(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	created, _ := s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "Old"})
	created.Title = "Updated"
	updated, err := s.Upsert(ctx, created)
	if err != nil || updated.Title != "Updated" {
		t.Fatalf("Upsert update: %v", err)
	}
}

func TestTaskStore_ListByUserPaginated_OffsetBeyondTotal(t *testing.T) {
	s := &taskStore{r: NewRegistry()}
	userID := uuid.New()
	s.Create(ctx, &model.Task{UserID: userID, Title: "T"}) //nolint:errcheck
	tasks, total, err := s.ListByUserPaginated(ctx, userID, store.Pagination{Offset: 100, Limit: 10})
	if err != nil || total != 1 || len(tasks) != 0 {
		t.Errorf("expected 0 tasks beyond offset: total=%d len=%d err=%v", total, len(tasks), err)
	}
}

func TestOrgStore_ListForUser_SkipsMissingOrg(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	userID := uuid.New()
	orphanOrgID := uuid.New()
	// Add member entry without corresponding org in orgs map
	r.members[orgMemberKey{orphanOrgID, userID}] = &model.OrgMember{OrgID: orphanOrgID, UserID: userID}
	orgs, err := s.ListForUser(ctx, userID)
	if err != nil || len(orgs) != 0 {
		t.Errorf("expected 0 orgs when org is missing: %v %v", len(orgs), err)
	}
}

func TestPageStore_ListByUserPaginated_WithinBound(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	userID := uuid.New()
	for i := 0; i < 4; i++ {
		s.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "P"}) //nolint:errcheck
	}
	pages, total, err := s.ListByUserPaginated(ctx, userID, store.Pagination{Offset: 0, Limit: 4})
	if err != nil || total != 4 || len(pages) != 4 {
		t.Errorf("pagination: total=%d len=%d err=%v", total, len(pages), err)
	}
}

// ─── Error-injection coverage ─────────────────────────────────────────────────

func TestPageStore_ListByUser_InjectedError(t *testing.T) {
	injErr := store.ErrNotFound
	s := &pageStore{r: NewRegistry(), listByUserErr: injErr}
	_, err := s.ListByUser(ctx, uuid.New())
	if err != injErr {
		t.Errorf("want injected error, got %v", err)
	}
}

func TestPageStore_ListByUserPaginated_InjectedError(t *testing.T) {
	injErr := store.ErrNotFound
	s := &pageStore{r: NewRegistry(), listByUserErr: injErr}
	_, _, err := s.ListByUserPaginated(ctx, uuid.New(), store.Pagination{Limit: 10})
	if err != injErr {
		t.Errorf("want injected error, got %v", err)
	}
}

func TestTaskStore_ListByUser_InjectedError(t *testing.T) {
	injErr := store.ErrNotFound
	s := &taskStore{r: NewRegistry(), listByUserErr: injErr}
	_, err := s.ListByUser(ctx, uuid.New())
	if err != injErr {
		t.Errorf("want injected error, got %v", err)
	}
}

func TestTaskStore_ListByUserPaginated_InjectedError(t *testing.T) {
	injErr := store.ErrNotFound
	s := &taskStore{r: NewRegistry(), listByUserErr: injErr}
	_, _, err := s.ListByUserPaginated(ctx, uuid.New(), store.Pagination{Limit: 10})
	if err != injErr {
		t.Errorf("want injected error, got %v", err)
	}
}

func TestTaskStore_ListBySourcePage_InjectedError(t *testing.T) {
	injErr := store.ErrNotFound
	s := &taskStore{r: NewRegistry(), listByUserErr: injErr}
	_, err := s.ListBySourcePage(ctx, uuid.New(), uuid.New())
	if err != injErr {
		t.Errorf("want injected error, got %v", err)
	}
}

func TestTaskStore_ListByFilter_InjectedError(t *testing.T) {
	injErr := store.ErrNotFound
	s := &taskStore{r: NewRegistry(), listByUserErr: injErr}
	_, err := s.ListByFilter(ctx, uuid.New(), model.FilterSet{})
	if err != injErr {
		t.Errorf("want injected error, got %v", err)
	}
}

// ListBySourceNode sort closure: requires 2+ tasks with the same sourceNodeID.
func TestTaskStore_ListBySourceNode_SortClosure(t *testing.T) {
	r := NewRegistry()
	s := &taskStore{r: r}
	userID := uuid.New()
	nodeID := "node-abc"
	for i := 0; i < 3; i++ {
		s.Create(ctx, &model.Task{ //nolint:errcheck
			UserID:       userID,
			Title:        "T",
			SourceNodeID: &nodeID,
		})
	}
	tasks, err := s.ListBySourceNode(ctx, userID, nodeID)
	if err != nil || len(tasks) < 2 {
		t.Errorf("ListBySourceNode sort: want >=2 tasks, got %d err=%v", len(tasks), err)
	}
}

// ListForUser sort closure: requires 2+ orgs for the same user.
func TestOrgStore_ListForUser_SortClosure(t *testing.T) {
	r := NewRegistry()
	s := &orgStore{r: r}
	userID := uuid.New()
	for i := 0; i < 3; i++ {
		org, _ := s.Create(ctx, &model.Organization{Name: "Org"})
		r.members[orgMemberKey{org.ID, userID}] = &model.OrgMember{OrgID: org.ID, UserID: userID, Role: model.OrgRoleOwner}
	}
	orgs, err := s.ListForUser(ctx, userID)
	if err != nil || len(orgs) < 2 {
		t.Errorf("ListForUser sort: want >=2 orgs, got %d err=%v", len(orgs), err)
	}
}
