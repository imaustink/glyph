package memstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// ─── Registry.canRead ─────────────────────────────────────────────────────────

func TestCanRead_OwnerAlwaysAllowed(t *testing.T) {
	r := NewRegistry()
	userID := uuid.New()
	if !r.canRead(userID, userID, nil, false, model.ShareResourcePage, uuid.New()) {
		t.Error("owner should have read access")
	}
}

func TestCanRead_OrgMemberNonPrivate(t *testing.T) {
	r := NewRegistry()
	ownerID := uuid.New()
	memberID := uuid.New()
	orgID := uuid.New()

	r.members[orgMemberKey{orgID, memberID}] = &model.OrgMember{
		OrgID: orgID, UserID: memberID, Role: model.OrgRoleViewer,
	}

	if !r.canRead(memberID, ownerID, &orgID, false, model.ShareResourcePage, uuid.New()) {
		t.Error("org member should read non-private resource")
	}
}

func TestCanRead_OrgMemberPrivateBlocked(t *testing.T) {
	r := NewRegistry()
	ownerID := uuid.New()
	memberID := uuid.New()
	orgID := uuid.New()

	r.members[orgMemberKey{orgID, memberID}] = &model.OrgMember{
		OrgID: orgID, UserID: memberID, Role: model.OrgRoleViewer,
	}

	if r.canRead(memberID, ownerID, &orgID, true, model.ShareResourcePage, uuid.New()) {
		t.Error("org member should NOT read private resource")
	}
}

func TestCanRead_NoOrgIDBlocksOrgCheck(t *testing.T) {
	r := NewRegistry()
	ownerID := uuid.New()
	memberID := uuid.New()
	orgID := uuid.New()

	r.members[orgMemberKey{orgID, memberID}] = &model.OrgMember{
		OrgID: orgID, UserID: memberID, Role: model.OrgRoleViewer,
	}

	// No orgID on resource — org membership should not grant access
	if r.canRead(memberID, ownerID, nil, false, model.ShareResourcePage, uuid.New()) {
		t.Error("without orgID on resource, membership should not grant access")
	}
}

func TestCanRead_DirectShareAllows(t *testing.T) {
	r := NewRegistry()
	ownerID := uuid.New()
	shareeID := uuid.New()
	resourceID := uuid.New()

	shareID := uuid.New()
	r.shares[shareID] = &model.Share{
		ID:           shareID,
		ResourceType: model.ShareResourcePage,
		ResourceID:   resourceID,
		SharedWith:   model.ShareUser{ID: shareeID},
		Permission:   model.SharePermissionViewer,
	}

	if !r.canRead(shareeID, ownerID, nil, false, model.ShareResourcePage, resourceID) {
		t.Error("direct share should grant read access")
	}
}

func TestCanRead_ShareWrongResourceDenied(t *testing.T) {
	r := NewRegistry()
	ownerID := uuid.New()
	shareeID := uuid.New()
	resourceID := uuid.New()
	otherResourceID := uuid.New()

	shareID := uuid.New()
	r.shares[shareID] = &model.Share{
		ID:           shareID,
		ResourceType: model.ShareResourcePage,
		ResourceID:   otherResourceID, // different resource
		SharedWith:   model.ShareUser{ID: shareeID},
		Permission:   model.SharePermissionViewer,
	}

	if r.canRead(shareeID, ownerID, nil, false, model.ShareResourcePage, resourceID) {
		t.Error("share for different resource should not grant access")
	}
}

func TestCanRead_NoAccessDenied(t *testing.T) {
	r := NewRegistry()
	ownerID := uuid.New()
	strangerID := uuid.New()

	if r.canRead(strangerID, ownerID, nil, false, model.ShareResourcePage, uuid.New()) {
		t.Error("stranger with no share should not have read access")
	}
}

// ─── userStore.Search ─────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }

func setupSearchUsers(r *Registry) (alice, bob, carol, dave uuid.UUID) {
	alice = uuid.New()
	bob = uuid.New()
	carol = uuid.New()
	dave = uuid.New()

	r.usersByID[alice] = &model.User{ID: alice, Email: strPtr("alice@example.com"), Name: strPtr("Alice Smith")}
	r.usersByID[bob] = &model.User{ID: bob, Email: strPtr("bob@example.com"), Name: strPtr("Bob Jones")}
	r.usersByID[carol] = &model.User{ID: carol, Email: strPtr("carol@other.org"), Name: strPtr("Carol White")}
	r.usersByID[dave] = &model.User{ID: dave, Email: strPtr("dave@example.com"), Name: strPtr("Dave Brown")}
	return
}

func TestSearch_MatchesByEmail(t *testing.T) {
	r := NewRegistry()
	alice, _, _, _ := setupSearchUsers(r)
	s := &userStore{r: r}

	results, err := s.Search(context.Background(), "alice@example", uuid.Nil, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].ID != alice {
		t.Errorf("expected alice, got %v", results)
	}
}

func TestSearch_MatchesByName(t *testing.T) {
	r := NewRegistry()
	_, bob, _, _ := setupSearchUsers(r)
	s := &userStore{r: r}

	results, err := s.Search(context.Background(), "bob", uuid.Nil, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].ID != bob {
		t.Errorf("expected bob, got %v", results)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	r := NewRegistry()
	alice, _, _, _ := setupSearchUsers(r)
	s := &userStore{r: r}

	results, err := s.Search(context.Background(), "ALICE", uuid.Nil, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].ID != alice {
		t.Errorf("expected alice, got %v", results)
	}
}

func TestSearch_ExcludesSpecifiedID(t *testing.T) {
	r := NewRegistry()
	alice, _, _, _ := setupSearchUsers(r)
	s := &userStore{r: r}

	results, err := s.Search(context.Background(), "alice", alice, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, res := range results {
		if res.ID == alice {
			t.Error("alice should be excluded from results")
		}
	}
}

func TestSearch_RespectsLimit(t *testing.T) {
	r := NewRegistry()
	setupSearchUsers(r)
	s := &userStore{r: r}

	results, err := s.Search(context.Background(), "example", uuid.Nil, nil, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestSearch_RestrictsToOrgMembers(t *testing.T) {
	r := NewRegistry()
	alice, bob, _, _ := setupSearchUsers(r)
	orgID := uuid.New()

	// Only alice is in the org
	r.members[orgMemberKey{orgID, alice}] = &model.OrgMember{OrgID: orgID, UserID: alice}

	s := &userStore{r: r}
	results, err := s.Search(context.Background(), "example", uuid.Nil, []uuid.UUID{orgID}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, res := range results {
		if res.ID == bob {
			t.Error("bob (not in org) should be excluded when orgIDs filter is provided")
		}
	}
	found := false
	for _, res := range results {
		if res.ID == alice {
			found = true
		}
	}
	if !found {
		t.Error("alice (in org) should appear in results")
	}
}

func TestSearch_SortedByName(t *testing.T) {
	r := NewRegistry()
	setupSearchUsers(r)
	s := &userStore{r: r}

	results, err := s.Search(context.Background(), "example", uuid.Nil, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(results); i++ {
		prev := ""
		curr := ""
		if results[i-1].Name != nil {
			prev = *results[i-1].Name
		}
		if results[i].Name != nil {
			curr = *results[i].Name
		}
		if prev > curr {
			t.Errorf("results not sorted by name: %q > %q", prev, curr)
		}
	}
}

// ─── pageStore.IsAncestor ──────────────────────────────────────────────────────

func setupPageTree(r *Registry) (root, child, grandchild, sibling uuid.UUID) {
	root = uuid.New()
	child = uuid.New()
	grandchild = uuid.New()
	sibling = uuid.New()

	now := time.Now()
	r.pages[root] = &model.Page{ID: root, UserID: uuid.New(), Type: model.NodeTypePage, CreatedAt: now, UpdatedAt: now}
	r.pages[child] = &model.Page{ID: child, UserID: uuid.New(), Type: model.NodeTypePage, ParentID: &root, CreatedAt: now, UpdatedAt: now}
	r.pages[grandchild] = &model.Page{ID: grandchild, UserID: uuid.New(), Type: model.NodeTypePage, ParentID: &child, CreatedAt: now, UpdatedAt: now}
	r.pages[sibling] = &model.Page{ID: sibling, UserID: uuid.New(), Type: model.NodeTypePage, ParentID: &root, CreatedAt: now, UpdatedAt: now}
	return
}

func TestIsAncestor_DirectParent(t *testing.T) {
	r := NewRegistry()
	_, child, _, _ := setupPageTree(r)
	root := *r.pages[child].ParentID
	s := &pageStore{r: r}

	ok, err := s.IsAncestor(context.Background(), root, child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("direct parent should be reported as ancestor")
	}
}

func TestIsAncestor_Grandparent(t *testing.T) {
	r := NewRegistry()
	root, _, grandchild, _ := setupPageTree(r)
	s := &pageStore{r: r}

	ok, err := s.IsAncestor(context.Background(), root, grandchild)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("grandparent should be reported as ancestor")
	}
}

func TestIsAncestor_SiblingIsNotAncestor(t *testing.T) {
	r := NewRegistry()
	_, child, _, sibling := setupPageTree(r)
	s := &pageStore{r: r}

	ok, err := s.IsAncestor(context.Background(), sibling, child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("sibling should not be reported as ancestor")
	}
}

func TestIsAncestor_DescendantIsNotAncestor(t *testing.T) {
	r := NewRegistry()
	root, child, _, _ := setupPageTree(r)
	s := &pageStore{r: r}

	// child is NOT an ancestor of root
	ok, err := s.IsAncestor(context.Background(), child, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("descendant should not be reported as ancestor of its own ancestor")
	}
}

func TestIsAncestor_NonExistentIDReturnsFalse(t *testing.T) {
	r := NewRegistry()
	s := &pageStore{r: r}

	ok, err := s.IsAncestor(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("non-existent IDs should return false")
	}
}

func TestIsAncestor_RootHasNoAncestor(t *testing.T) {
	r := NewRegistry()
	root, _, _, _ := setupPageTree(r)
	s := &pageStore{r: r}

	// root has no parent — nothing is its ancestor
	ok, err := s.IsAncestor(context.Background(), uuid.New(), root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("root node has no ancestors")
	}
}

// ─── New*Store constructors ───────────────────────────────────────────────────

func TestNewStoreConstructors_ReturnNonNil(t *testing.T) {
	if s := NewUserStore(); s == nil {
		t.Error("NewUserStore returned nil")
	}
	if s := NewPageStore(); s == nil {
		t.Error("NewPageStore returned nil")
	}
	if s := NewTaskStore(); s == nil {
		t.Error("NewTaskStore returned nil")
	}
	if s := NewLaneStore(); s == nil {
		t.Error("NewLaneStore returned nil")
	}
	if s := NewTemplateStore(); s == nil {
		t.Error("NewTemplateStore returned nil")
	}
	if s := NewOrgStore(); s == nil {
		t.Error("NewOrgStore returned nil")
	}
	if s := NewShareStore(); s == nil {
		t.Error("NewShareStore returned nil")
	}
}

// ─── pageStore.Upsert — mismatched UserID branch ──────────────────────────────

func TestPageStore_Upsert_MismatchedUserID_ReturnsError(t *testing.T) {
	r := NewRegistry()
	pages := &pageStore{r: r}
	ctx := context.Background()

	ownerID := uuid.New()
	p, err := pages.Create(ctx, &model.Page{
		UserID: ownerID,
		Type:   model.NodeTypePage,
		Title:  "Original",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Attempt to upsert with a different UserID — should be denied
	p.UserID = uuid.New()
	_, err = pages.Upsert(ctx, p)
	if err == nil {
		t.Error("expected error when upserting with wrong UserID")
	}
}

// ─── pageStore.UpsertContent — error branches ─────────────────────────────────

func TestPageStore_UpsertContent_PageNotFound_ReturnsError(t *testing.T) {
	r := NewRegistry()
	pages := &pageStore{r: r}
	ctx := context.Background()

	_, err := pages.UpsertContent(ctx, &model.PageContent{
		PageID:  uuid.New(), // non-existent page
		Content: json.RawMessage(`{"type":"doc"}`),
	}, uuid.New())
	if err == nil {
		t.Error("expected error for non-existent page")
	}
}

func TestPageStore_UpsertContent_WrongUser_ReturnsError(t *testing.T) {
	r := NewRegistry()
	pages := &pageStore{r: r}
	ctx := context.Background()

	ownerID := uuid.New()
	p, err := pages.Create(ctx, &model.Page{UserID: ownerID, Type: model.NodeTypePage, Title: "Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = pages.UpsertContent(ctx, &model.PageContent{
		PageID:  p.ID,
		Content: json.RawMessage(`{"type":"doc"}`),
	}, uuid.New())
	if err == nil {
		t.Error("expected error when upserting content with wrong user")
	}
}

// ─── pageStore.ListByUserPaginated — offset edge cases ───────────────────────

func TestPageStore_ListByUserPaginated_OffsetBeyondTotal(t *testing.T) {
	r := NewRegistry()
	pages := &pageStore{r: r}
	ctx := context.Background()
	userID := uuid.New()

	for i := 0; i < 2; i++ {
		_, err := pages.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "Page"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	result, total, err := pages.ListByUserPaginated(ctx, userID, store.Pagination{Offset: 100, Limit: 10})
	if err != nil {
		t.Fatalf("ListByUserPaginated: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice for offset beyond total, got %d items", len(result))
	}
}

func TestPageStore_ListByUserPaginated_LimitBeyondEnd(t *testing.T) {
	r := NewRegistry()
	pages := &pageStore{r: r}
	ctx := context.Background()
	userID := uuid.New()

	for i := 0; i < 3; i++ {
		_, err := pages.Create(ctx, &model.Page{UserID: userID, Type: model.NodeTypePage, Title: "Page"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	result, total, err := pages.ListByUserPaginated(ctx, userID, store.Pagination{Offset: 1, Limit: 100})
	if err != nil {
		t.Fatalf("ListByUserPaginated: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

// ─── memstoreToStringSlice — default branch ───────────────────────────────────

func TestMemstoreToStringSlice_DefaultBranch(t *testing.T) {
	result := memstoreToStringSlice("hello")
	if len(result) != 1 || result[0] != "hello" {
		t.Errorf("expected [\"hello\"], got %v", result)
	}
}

func TestMemstoreToStringSlice_NilValue(t *testing.T) {
	result := memstoreToStringSlice(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestMemstoreToStringSlice_InterfaceSlice(t *testing.T) {
	result := memstoreToStringSlice([]interface{}{"a", "b"})
	if len(result) != 2 || result[0] != "a" {
		t.Errorf("expected [a b], got %v", result)
	}
}

func TestMemstoreToStringSlice_StringSlice(t *testing.T) {
	result := memstoreToStringSlice([]string{"x", "y"})
	if len(result) != 2 || result[0] != "x" {
		t.Errorf("expected [x y], got %v", result)
	}
}

// ─── memstoreField — default and nil branches ─────────────────────────────────

func TestMemstoreField_UnknownField_ReturnsNil(t *testing.T) {
	task := &model.Task{Status: model.StatusTodo}
	if result := memstoreField(task, "unknownField"); result != nil {
		t.Errorf("expected nil for unknown field, got %v", result)
	}
}

func TestMemstoreField_DueDate_Nil(t *testing.T) {
	task := &model.Task{DueDate: nil}
	if result := memstoreField(task, "dueDate"); result != nil {
		t.Errorf("expected nil for nil DueDate, got %v", result)
	}
}

func TestMemstoreField_SourcePageID_Nil(t *testing.T) {
	task := &model.Task{SourcePageID: nil}
	if result := memstoreField(task, "sourcePageId"); result != nil {
		t.Errorf("expected nil for nil SourcePageID, got %v", result)
	}
}

// ─── userStore.Upsert — update path ──────────────────────────────────────────

func TestUserStore_Upsert_UpdateExistingUser(t *testing.T) {
s := &userStore{r: NewRegistry()}
ctx := context.Background()

email1 := "user@example.com"
name1 := "Alice"
u1, err := s.Upsert(ctx, "sub1", "https://issuer.example.com", &email1, &name1)
if err != nil {
t.Fatalf("first Upsert: %v", err)
}

// Update with new email/name for same sub+issuer
email2 := "alice@example.com"
name2 := "Alice Smith"
u2, err := s.Upsert(ctx, "sub1", "https://issuer.example.com", &email2, &name2)
if err != nil {
t.Fatalf("second Upsert: %v", err)
}

if u1.ID != u2.ID {
t.Error("second Upsert should return the same user ID")
}
if u2.Email == nil || *u2.Email != email2 {
t.Errorf("Email not updated: got %v, want %q", u2.Email, email2)
}
if u2.Name == nil || *u2.Name != name2 {
t.Errorf("Name not updated: got %v, want %q", u2.Name, name2)
}
}

// ─── cloneTask — all nullable fields ─────────────────────────────────────────

func TestCloneTask_AllNullableFields(t *testing.T) {
s := &taskStore{r: NewRegistry()}
ctx := context.Background()

userID := uuid.New()
srcPageID := uuid.New()
srcNodeID := "node-123"
due := "2024-12-31"
orgID := uuid.New()
linkTitle := "Example"

task, err := s.Create(ctx, &model.Task{
UserID:       userID,
Title:        "Test",
Tags:         []string{"a", "b"},
DueDate:      &due,
SourcePageID: &srcPageID,
SourceNodeID: &srcNodeID,
Link:         &model.LinkMeta{URL: "https://example.com", Title: &linkTitle},
OrgID:        &orgID,
})
if err != nil {
t.Fatalf("Create: %v", err)
}
if task.OrgID == nil || *task.OrgID != orgID {
t.Errorf("OrgID not preserved: got %v, want %v", task.OrgID, orgID)
}
if task.Link == nil || task.Link.URL != "https://example.com" {
t.Errorf("Link not preserved: got %v", task.Link)
}
}

// ─── taskStore.ListBySourceNode — canRead false branch ───────────────────────

func TestTaskStore_ListBySourceNode_HidesInaccessibleTasks(t *testing.T) {
s := &taskStore{r: NewRegistry()}
ctx := context.Background()

ownerID := uuid.New()
otherUserID := uuid.New()
srcNodeID := "shared-node"

// Create a task owned by ownerID with a sourceNodeID
_, err := s.Create(ctx, &model.Task{
UserID:       ownerID,
Title:        "Owner task",
SourceNodeID: &srcNodeID,
IsPrivate:    true, // private — other users can't see it
})
if err != nil {
t.Fatalf("Create: %v", err)
}

// otherUserID should not see the private task
results, err := s.ListBySourceNode(ctx, otherUserID, srcNodeID)
if err != nil {
t.Fatalf("ListBySourceNode: %v", err)
}
if len(results) != 0 {
t.Errorf("expected 0 results for inaccessible task, got %d", len(results))
}

// ownerID should see their own task
results, err = s.ListBySourceNode(ctx, ownerID, srcNodeID)
if err != nil {
t.Fatalf("ListBySourceNode: %v", err)
}
if len(results) != 1 {
t.Errorf("expected 1 result for owner, got %d", len(results))
}
}
