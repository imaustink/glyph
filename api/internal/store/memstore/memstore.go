// Package memstore provides a pure in-memory implementation of all store
// interfaces. It is useful for tests that need to run without a database.
//
// All stores share a single Registry that holds the backing data, which
// allows cross-store access checks (e.g. page reads that include pages shared
// via org membership or direct shares) to work correctly in tests.
package memstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// ─── Registry ─────────────────────────────────────────────────────────────────

type orgMemberKey struct{ orgID, userID uuid.UUID }

// Registry holds all in-memory data so that stores can perform cross-entity
// access checks (e.g. checking org membership from within the page store).
type Registry struct {
	mu sync.RWMutex

	usersByID  map[uuid.UUID]*model.User
	usersBySub map[string]*model.User

	pages    map[uuid.UUID]*model.Page
	contents map[uuid.UUID]*model.PageContent

	tasks     map[uuid.UUID]*model.Task
	lanes     map[uuid.UUID]*model.Lane
	templates map[uuid.UUID]*model.Template

	orgs    map[uuid.UUID]*model.Organization
	members map[orgMemberKey]*model.OrgMember
	shares  map[uuid.UUID]*model.Share
}

func NewRegistry() *Registry {
	r := &Registry{}
	r.init()
	return r
}

func (r *Registry) init() {
	r.usersByID = make(map[uuid.UUID]*model.User)
	r.usersBySub = make(map[string]*model.User)
	r.pages = make(map[uuid.UUID]*model.Page)
	r.contents = make(map[uuid.UUID]*model.PageContent)
	r.tasks = make(map[uuid.UUID]*model.Task)
	r.lanes = make(map[uuid.UUID]*model.Lane)
	r.templates = make(map[uuid.UUID]*model.Template)
	r.orgs = make(map[uuid.UUID]*model.Organization)
	r.members = make(map[orgMemberKey]*model.OrgMember)
	r.shares = make(map[uuid.UUID]*model.Share)
}

// Reset clears all data. Called between tests.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
}

// canRead returns true if userID has read access. Must be called with lock held.
func (r *Registry) canRead(userID, ownerID uuid.UUID, orgID *uuid.UUID, isPrivate bool, resourceType model.ShareResourceType, resourceID uuid.UUID) bool {
	if userID == ownerID {
		return true
	}
	if orgID != nil && !isPrivate {
		if _, ok := r.members[orgMemberKey{*orgID, userID}]; ok {
			return true
		}
	}
	for _, s := range r.shares {
		if s.ResourceType == resourceType && s.ResourceID == resourceID && s.SharedWith.ID == userID {
			return true
		}
	}
	return false
}

// ─── Factory functions ────────────────────────────────────────────────────────

// NewStores creates all store implementations backed by the same Registry.
func NewStores() (store.UserStore, store.PageStore, store.TaskStore, store.LaneStore, store.TemplateStore, store.OrgStore, store.ShareStore) {
	r := NewRegistry()
	return &userStore{r: r}, &pageStore{r: r}, &taskStore{r: r},
		&laneStore{r: r}, &templateStore{r: r}, &orgStore{r: r}, &shareStore{r: r}
}

// Convenience constructors that each own a private Registry.
func NewUserStore() store.UserStore         { return &userStore{r: NewRegistry()} }
func NewPageStore() store.PageStore         { return &pageStore{r: NewRegistry()} }
func NewTaskStore() store.TaskStore         { return &taskStore{r: NewRegistry()} }
func NewLaneStore() store.LaneStore         { return &laneStore{r: NewRegistry()} }
func NewTemplateStore() store.TemplateStore { return &templateStore{r: NewRegistry()} }
func NewOrgStore() store.OrgStore           { return &orgStore{r: NewRegistry()} }
func NewShareStore() store.ShareStore       { return &shareStore{r: NewRegistry()} }

// Resettable is implemented by all memstore types.
type Resettable interface{ Reset() }

// ─── Clone helpers ────────────────────────────────────────────────────────────

func clonePage(p *model.Page) *model.Page {
	cp := *p
	if p.Tags != nil {
		cp.Tags = make([]string, len(p.Tags))
		copy(cp.Tags, p.Tags)
	}
	if p.ParentID != nil {
		id := *p.ParentID
		cp.ParentID = &id
	}
	if p.OrgID != nil {
		id := *p.OrgID
		cp.OrgID = &id
	}
	if p.TodoTrigger != nil {
		t := *p.TodoTrigger
		if p.TodoTrigger.BlockTypes != nil {
			t.BlockTypes = make([]string, len(p.TodoTrigger.BlockTypes))
			copy(t.BlockTypes, p.TodoTrigger.BlockTypes)
		}
		cp.TodoTrigger = &t
	}
	return &cp
}

func cloneTask(t *model.Task) *model.Task {
	cp := *t
	if t.Tags != nil {
		cp.Tags = make([]string, len(t.Tags))
		copy(cp.Tags, t.Tags)
	}
	if t.DueDate != nil {
		d := *t.DueDate
		cp.DueDate = &d
	}
	if t.SourcePageID != nil {
		id := *t.SourcePageID
		cp.SourcePageID = &id
	}
	if t.SourceNodeID != nil {
		n := *t.SourceNodeID
		cp.SourceNodeID = &n
	}
	if t.Link != nil {
		lm := *t.Link
		cp.Link = &lm
	}
	if t.OrgID != nil {
		id := *t.OrgID
		cp.OrgID = &id
	}
	return &cp
}

func cloneLane(l *model.Lane) *model.Lane {
	cp := *l
	if l.FilterSet.Rules != nil {
		cp.FilterSet.Rules = make([]model.FilterRule, len(l.FilterSet.Rules))
		copy(cp.FilterSet.Rules, l.FilterSet.Rules)
	}
	if l.SortConfig.Field != nil {
		f := *l.SortConfig.Field
		cp.SortConfig.Field = &f
	}
	if l.SortConfig.Direction != nil {
		d := *l.SortConfig.Direction
		cp.SortConfig.Direction = &d
	}
	if l.SortConfig.TaskOrder != nil {
		cp.SortConfig.TaskOrder = make([]string, len(l.SortConfig.TaskOrder))
		copy(cp.SortConfig.TaskOrder, l.SortConfig.TaskOrder)
	}
	return &cp
}

func cloneTemplate(t *model.Template) *model.Template {
	cp := *t
	if t.TodoTrigger != nil {
		trig := *t.TodoTrigger
		if t.TodoTrigger.BlockTypes != nil {
			trig.BlockTypes = make([]string, len(t.TodoTrigger.BlockTypes))
			copy(trig.BlockTypes, t.TodoTrigger.BlockTypes)
		}
		cp.TodoTrigger = &trig
	}
	if t.OrgID != nil {
		id := *t.OrgID
		cp.OrgID = &id
	}
	return &cp
}

func cloneOrg(o *model.Organization) *model.Organization { cp := *o; return &cp }
func cloneMember(m *model.OrgMember) *model.OrgMember    { cp := *m; return &cp }
func cloneShare(s *model.Share) *model.Share             { cp := *s; return &cp }

// ─── UserStore ────────────────────────────────────────────────────────────────

type userStore struct{ r *Registry }

func (s *userStore) Upsert(_ context.Context, sub, issuer string, email, name *string) (*model.User, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	key := sub + "|" + issuer
	if u, ok := s.r.usersBySub[key]; ok {
		u.Email = email
		u.Name = name
		u.UpdatedAt = time.Now()
		cp := *u
		return &cp, nil
	}
	u := &model.User{
		ID: uuid.New(), Sub: sub, Issuer: issuer, Email: email, Name: name,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	s.r.usersByID[u.ID] = u
	s.r.usersBySub[key] = u
	cp := *u
	return &cp, nil
}

func (s *userStore) GetByID(_ context.Context, id uuid.UUID) (*model.User, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	u, ok := s.r.usersByID[id]
	if !ok {
		return nil, fmt.Errorf("user get: not found")
	}
	cp := *u
	return &cp, nil
}

func (s *userStore) GetByEmail(_ context.Context, email string) (*model.User, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	for _, u := range s.r.usersByID {
		if u.Email != nil && strings.EqualFold(*u.Email, email) {
			cp := *u
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("user get by email: not found")
}

func (s *userStore) Search(_ context.Context, query string, excludeID uuid.UUID, orgIDs []uuid.UUID, limit int) ([]*model.UserSearchResult, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	q := strings.ToLower(query)

	// Build set of allowed user IDs when orgIDs filter is provided.
	allowedIDs := map[uuid.UUID]bool{}
	if len(orgIDs) > 0 {
		orgIDSet := map[uuid.UUID]bool{}
		for _, id := range orgIDs {
			orgIDSet[id] = true
		}
		for k, m := range s.r.members {
			if orgIDSet[k.orgID] {
				allowedIDs[m.UserID] = true
			}
		}
	}

	results := make([]*model.UserSearchResult, 0)
	for _, u := range s.r.usersByID {
		if u.ID == excludeID {
			continue
		}
		if len(orgIDs) > 0 && !allowedIDs[u.ID] {
			continue
		}
		emailMatch := u.Email != nil && strings.Contains(strings.ToLower(*u.Email), q)
		nameMatch := u.Name != nil && strings.Contains(strings.ToLower(*u.Name), q)
		if emailMatch || nameMatch {
			results = append(results, &model.UserSearchResult{ID: u.ID, Email: u.Email, Name: u.Name})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		ni, nj := "", ""
		if results[i].Name != nil {
			ni = *results[i].Name
		}
		if results[j].Name != nil {
			nj = *results[j].Name
		}
		return ni < nj
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *userStore) Reset() { s.r.Reset() }

// ─── PageStore ────────────────────────────────────────────────────────────────

type pageStore struct {
	r             *Registry
	listByUserErr error // set only in tests to inject a ListByUser error
}

func (s *pageStore) ListByUser(_ context.Context, userID uuid.UUID) ([]*model.Page, error) {
	if s.listByUserErr != nil {
		return nil, s.listByUserErr
	}
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.Page, 0)
	for _, p := range s.r.pages {
		if s.r.canRead(userID, p.UserID, p.OrgID, p.IsPrivate, model.ShareResourcePage, p.ID) {
			result = append(result, clonePage(p))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result, nil
}

func (s *pageStore) ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg store.Pagination) ([]*model.Page, int, error) {
	all, err := s.ListByUser(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	start := pg.Offset
	if start > total {
		start = total
	}
	end := start + pg.Limit
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

func (s *pageStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Page, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	p, ok := s.r.pages[id]
	if !ok || !s.r.canRead(userID, p.UserID, p.OrgID, p.IsPrivate, model.ShareResourcePage, p.ID) {
		return nil, fmt.Errorf("pages get: not found")
	}
	return clonePage(p), nil
}

func (s *pageStore) GetFolderByID(_ context.Context, id, userID uuid.UUID) (*model.Page, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	p, ok := s.r.pages[id]
	if !ok || !s.r.canRead(userID, p.UserID, p.OrgID, p.IsPrivate, model.ShareResourceFolder, p.ID) {
		return nil, fmt.Errorf("pages get folder: not found")
	}
	return clonePage(p), nil
}

func (s *pageStore) Upsert(_ context.Context, p *model.Page) (*model.Page, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if existing, ok := s.r.pages[p.ID]; ok {
		if existing.UserID != p.UserID {
			return nil, store.ErrNotFound
		}
		p.CreatedAt = existing.CreatedAt
		p.UpdatedAt = time.Now()
		stored := clonePage(p)
		s.r.pages[stored.ID] = stored
		return clonePage(stored), nil
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	stored := clonePage(p)
	s.r.pages[stored.ID] = stored
	return clonePage(stored), nil
}
func (s *pageStore) Create(_ context.Context, p *model.Page) (*model.Page, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	stored := clonePage(p)
	s.r.pages[stored.ID] = stored
	return clonePage(stored), nil
}

func (s *pageStore) Update(_ context.Context, p *model.Page) (*model.Page, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.pages[p.ID]
	if !ok || existing.UserID != p.UserID {
		return nil, fmt.Errorf("pages update: not found")
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now()
	stored := clonePage(p)
	s.r.pages[stored.ID] = stored
	return clonePage(stored), nil
}

func (s *pageStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	p, ok := s.r.pages[id]
	if !ok || p.UserID != userID {
		return nil
	}
	delete(s.r.pages, id)
	delete(s.r.contents, id)
	return nil
}

func (s *pageStore) GetContent(_ context.Context, pageID, userID uuid.UUID) (*model.PageContent, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	p, ok := s.r.pages[pageID]
	if !ok || !s.r.canRead(userID, p.UserID, p.OrgID, p.IsPrivate, model.ShareResourcePage, p.ID) {
		return nil, fmt.Errorf("get content: not found")
	}
	pc, ok := s.r.contents[pageID]
	if !ok {
		return nil, fmt.Errorf("get content: not found")
	}
	cp := *pc
	return &cp, nil
}

func (s *pageStore) UpsertContent(_ context.Context, pc *model.PageContent, userID uuid.UUID) (*model.PageContent, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	p, ok := s.r.pages[pc.PageID]
	if !ok {
		return nil, fmt.Errorf("upsert content — page lookup: not found")
	}
	if p.UserID != userID {
		return nil, fmt.Errorf("upsert content: forbidden")
	}
	stored := &model.PageContent{
		PageID:        pc.PageID,
		Content:       pc.Content,
		UpdatedAt:     time.Now(),
		SchemaVersion: pc.SchemaVersion,
	}
	s.r.contents[pc.PageID] = stored
	cp := *stored
	return &cp, nil
}

// IsAncestor walks the parent_id chain from nodeID upward and reports whether
// candidateAncestorID appears as an ancestor.
func (s *pageStore) IsAncestor(_ context.Context, candidateAncestorID, nodeID uuid.UUID) (bool, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	current := nodeID
	for {
		p, ok := s.r.pages[current]
		if !ok || p.ParentID == nil {
			return false, nil
		}
		if *p.ParentID == candidateAncestorID {
			return true, nil
		}
		current = *p.ParentID
	}
}

func (s *pageStore) GetDescendantIDs(_ context.Context, folderID uuid.UUID) ([]uuid.UUID, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	// BFS to collect all descendants.
	result := []uuid.UUID{folderID}
	queue := []uuid.UUID{folderID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, p := range s.r.pages {
			if p.ParentID != nil && *p.ParentID == current {
				result = append(result, p.ID)
				queue = append(queue, p.ID)
			}
		}
	}
	return result, nil
}

func (s *pageStore) Reset() { s.r.Reset() }

// ─── TaskStore ────────────────────────────────────────────────────────────────

type taskStore struct {
	r             *Registry
	listByUserErr error // set only in tests to inject a ListByUser error
}

func (s *taskStore) ListByUser(_ context.Context, userID uuid.UUID) ([]*model.Task, error) {
	if s.listByUserErr != nil {
		return nil, s.listByUserErr
	}
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.Task, 0)
	for _, t := range s.r.tasks {
		if s.r.canRead(userID, t.UserID, t.OrgID, t.IsPrivate, model.ShareResourceTask, t.ID) {
			result = append(result, cloneTask(t))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result, nil
}

func (s *taskStore) ListByUserPaginated(ctx context.Context, userID uuid.UUID, pg store.Pagination) ([]*model.Task, int, error) {
	all, err := s.ListByUser(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	start := pg.Offset
	if start > total {
		start = total
	}
	end := start + pg.Limit
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

func (s *taskStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Task, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	t, ok := s.r.tasks[id]
	if !ok || !s.r.canRead(userID, t.UserID, t.OrgID, t.IsPrivate, model.ShareResourceTask, t.ID) {
		return nil, fmt.Errorf("tasks get: not found")
	}
	return cloneTask(t), nil
}

func (s *taskStore) ListBySourcePage(ctx context.Context, userID uuid.UUID, pageID uuid.UUID) ([]*model.Task, error) {
	all, err := s.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]*model.Task, 0)
	for _, t := range all {
		if t.SourcePageID != nil && *t.SourcePageID == pageID {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *taskStore) ListBySourceNode(_ context.Context, userID uuid.UUID, sourceNodeID string) ([]*model.Task, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	var result []*model.Task
	for _, t := range s.r.tasks {
		if t.SourceNodeID != nil && *t.SourceNodeID == sourceNodeID {
			if s.r.canRead(userID, t.UserID, t.OrgID, t.IsPrivate, model.ShareResourceTask, t.ID) {
				result = append(result, cloneTask(t))
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result, nil
}

func (s *taskStore) Upsert(_ context.Context, t *model.Task) (*model.Task, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if existing, ok := s.r.tasks[t.ID]; ok {
		if existing.UserID != t.UserID {
			return nil, store.ErrNotFound
		}
		t.CreatedAt = existing.CreatedAt
		t.UpdatedAt = time.Now()
		clone := cloneTask(t)
		s.r.tasks[t.ID] = clone
		return cloneTask(clone), nil
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	clone := cloneTask(t)
	s.r.tasks[t.ID] = clone
	return cloneTask(clone), nil
}
func (s *taskStore) Create(_ context.Context, t *model.Task) (*model.Task, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	stored := cloneTask(t)
	s.r.tasks[stored.ID] = stored
	return cloneTask(stored), nil
}

func (s *taskStore) Update(_ context.Context, t *model.Task) (*model.Task, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.tasks[t.ID]
	if !ok || existing.UserID != t.UserID {
		return nil, fmt.Errorf("tasks update: not found")
	}
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = time.Now()
	stored := cloneTask(t)
	s.r.tasks[stored.ID] = stored
	return cloneTask(stored), nil
}

func (s *taskStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	t, ok := s.r.tasks[id]
	if !ok || t.UserID != userID {
		return nil
	}
	delete(s.r.tasks, id)
	return nil
}

func (s *taskStore) ListByFilter(ctx context.Context, userID uuid.UUID, fs model.FilterSet) ([]*model.Task, error) {
	all, err := s.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(fs.Rules) == 0 {
		return all, nil
	}
	result := make([]*model.Task, 0)
	for _, t := range all {
		if memstoreTaskMatchesFilter(t, fs) {
			result = append(result, t)
		}
	}
	return result, nil
}

// memstoreTaskMatchesFilter applies the FilterSet in-process, mirroring the
// client-side filterUtils logic so that memstore integration tests work correctly.
func memstoreTaskMatchesFilter(t *model.Task, fs model.FilterSet) bool {
	results := make([]bool, 0, len(fs.Rules))
	for _, rule := range fs.Rules {
		results = append(results, memstoreMatchesRule(t, rule))
	}
	if fs.Conjunction == model.ConjunctionOr {
		for _, r := range results {
			if r {
				return true
			}
		}
		return false
	}
	for _, r := range results {
		if !r {
			return false
		}
	}
	return true
}

func memstoreMatchesRule(t *model.Task, rule model.FilterRule) bool {
	switch rule.Operator {
	case model.FilterOpAny:
		return true
	case model.FilterOpEq:
		return fmt.Sprint(memstoreField(t, rule.Field)) == fmt.Sprint(rule.Value)
	case model.FilterOpNeq:
		return fmt.Sprint(memstoreField(t, rule.Field)) != fmt.Sprint(rule.Value)
	case model.FilterOpIn:
		vals := memstoreToStringSlice(rule.Value)
		v := fmt.Sprint(memstoreField(t, rule.Field))
		for _, s := range vals {
			if s == v {
				return true
			}
		}
		return false
	case model.FilterOpNotIn:
		vals := memstoreToStringSlice(rule.Value)
		v := fmt.Sprint(memstoreField(t, rule.Field))
		for _, s := range vals {
			if s == v {
				return false
			}
		}
		return true
	case model.FilterOpContains:
		if rule.Field == "tags" {
			needle := fmt.Sprint(rule.Value)
			for _, tag := range t.Tags {
				if tag == needle {
					return true
				}
			}
			return false
		}
		return strings.Contains(strings.ToLower(fmt.Sprint(memstoreField(t, rule.Field))), strings.ToLower(fmt.Sprint(rule.Value)))
	case model.FilterOpExists:
		v := memstoreField(t, rule.Field)
		return v != nil && fmt.Sprint(v) != ""
	case model.FilterOpNotExists:
		v := memstoreField(t, rule.Field)
		return v == nil || fmt.Sprint(v) == ""
	default:
		return true
	}
}

func memstoreField(t *model.Task, field string) interface{} {
	switch field {
	case "status":
		return string(t.Status)
	case "priority":
		return string(t.Priority)
	case "title":
		return t.Title
	case "dueDate":
		if t.DueDate == nil {
			return nil
		}
		return *t.DueDate
	case "sourcePageId":
		if t.SourcePageID == nil {
			return nil
		}
		return t.SourcePageID.String()
	case "tags":
		return t.Tags
	default:
		return nil
	}
}

func memstoreToStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []interface{}:
		result := make([]string, len(t))
		for i, item := range t {
			result[i] = fmt.Sprint(item)
		}
		return result
	case []string:
		return t
	default:
		s := fmt.Sprint(v)
		if s == "" || s == "<nil>" {
			return nil
		}
		return []string{s}
	}
}

func (s *taskStore) Reset() { s.r.Reset() }

func (s *taskStore) ListByFolder(_ context.Context, folderID uuid.UUID, descendantPageIDs []uuid.UUID) ([]*model.Task, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	pageIDSet := make(map[uuid.UUID]bool, len(descendantPageIDs))
	for _, id := range descendantPageIDs {
		pageIDSet[id] = true
	}
	result := make([]*model.Task, 0)
	for _, t := range s.r.tasks {
		inFolder := (t.FolderID != nil && *t.FolderID == folderID)
		inDescendant := (t.SourcePageID != nil && pageIDSet[*t.SourcePageID])
		if inFolder || inDescendant {
			cloned := *t
			result = append(result, &cloned)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result, nil
}

// ─── LaneStore ────────────────────────────────────────────────────────────────

type laneStore struct{ r *Registry }

func (s *laneStore) ListByUser(_ context.Context, userID uuid.UUID) ([]*model.Lane, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.Lane, 0)
	for _, l := range s.r.lanes {
		if l.UserID == userID {
			result = append(result, cloneLane(l))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result, nil
}

func (s *laneStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Lane, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	l, ok := s.r.lanes[id]
	if !ok || l.UserID != userID {
		return nil, fmt.Errorf("lanes get: not found")
	}
	return cloneLane(l), nil
}

func (s *laneStore) Upsert(_ context.Context, l *model.Lane) (*model.Lane, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	if existing, ok := s.r.lanes[l.ID]; ok {
		if existing.UserID != l.UserID {
			return nil, store.ErrNotFound
		}
		l.CreatedAt = existing.CreatedAt
		l.UpdatedAt = time.Now()
		stored := cloneLane(l)
		s.r.lanes[stored.ID] = stored
		return cloneLane(stored), nil
	}
	now := time.Now()
	l.CreatedAt = now
	l.UpdatedAt = now
	stored := cloneLane(l)
	s.r.lanes[stored.ID] = stored
	return cloneLane(stored), nil
}

func (s *laneStore) ReorderAll(_ context.Context, userID uuid.UUID, items []store.LaneReorderItem) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	for _, item := range items {
		if l, ok := s.r.lanes[item.ID]; ok && l.UserID == userID {
			clone := cloneLane(l)
			clone.Order = item.Order
			clone.UpdatedAt = time.Now()
			s.r.lanes[item.ID] = clone
		}
	}
	return nil
}
func (s *laneStore) Create(_ context.Context, l *model.Lane) (*model.Lane, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	now := time.Now()
	l.CreatedAt = now
	l.UpdatedAt = now
	stored := cloneLane(l)
	s.r.lanes[stored.ID] = stored
	return cloneLane(stored), nil
}

func (s *laneStore) BatchCreate(_ context.Context, lanes []*model.Lane) ([]*model.Lane, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	result := make([]*model.Lane, 0, len(lanes))
	now := time.Now()
	for _, l := range lanes {
		if l.ID == uuid.Nil {
			l.ID = uuid.New()
		}
		l.CreatedAt = now
		l.UpdatedAt = now
		stored := cloneLane(l)
		s.r.lanes[stored.ID] = stored
		result = append(result, cloneLane(stored))
	}
	return result, nil
}

func (s *laneStore) Update(_ context.Context, l *model.Lane) (*model.Lane, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.lanes[l.ID]
	if !ok || existing.UserID != l.UserID {
		return nil, fmt.Errorf("lanes update: not found")
	}
	l.CreatedAt = existing.CreatedAt
	l.UpdatedAt = time.Now()
	stored := cloneLane(l)
	s.r.lanes[stored.ID] = stored
	return cloneLane(stored), nil
}

func (s *laneStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	l, ok := s.r.lanes[id]
	if !ok || l.UserID != userID {
		return nil
	}
	delete(s.r.lanes, id)
	return nil
}

func (s *laneStore) ListByFolder(_ context.Context, folderID, _ uuid.UUID) ([]*model.Lane, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.Lane, 0)
	for _, l := range s.r.lanes {
		if l.FolderID != nil && *l.FolderID == folderID {
			cloned := *l
			result = append(result, &cloned)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result, nil
}

func (s *laneStore) GetByIDAndFolder(_ context.Context, id, folderID uuid.UUID) (*model.Lane, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	l, ok := s.r.lanes[id]
	if !ok || l.FolderID == nil || *l.FolderID != folderID {
		return nil, store.ErrNotFound
	}
	cloned := *l
	return &cloned, nil
}

func (s *laneStore) UpdateByIDAndFolder(_ context.Context, l *model.Lane, folderID uuid.UUID) (*model.Lane, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.lanes[l.ID]
	if !ok || existing.FolderID == nil || *existing.FolderID != folderID {
		return nil, store.ErrNotFound
	}
	existing.Title = l.Title
	existing.FilterSet = l.FilterSet
	existing.SortConfig = l.SortConfig
	existing.Order = l.Order
	existing.UpdatedAt = time.Now()
	cloned := *existing
	return &cloned, nil
}

func (s *laneStore) DeleteByIDAndFolder(_ context.Context, id, folderID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	l, ok := s.r.lanes[id]
	if !ok || l.FolderID == nil || *l.FolderID != folderID {
		return store.ErrNotFound
	}
	delete(s.r.lanes, id)
	return nil
}

func (s *laneStore) Reset() { s.r.Reset() }

// ─── TemplateStore ────────────────────────────────────────────────────────────

type templateStore struct{ r *Registry }

func (s *templateStore) ListByUser(_ context.Context, userID uuid.UUID) ([]*model.Template, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.Template, 0)
	for _, t := range s.r.templates {
		if s.r.canRead(userID, t.UserID, t.OrgID, t.IsPrivate, model.ShareResourceTemplate, t.ID) {
			result = append(result, cloneTemplate(t))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (s *templateStore) GetByID(_ context.Context, id, userID uuid.UUID) (*model.Template, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	t, ok := s.r.templates[id]
	if !ok || !s.r.canRead(userID, t.UserID, t.OrgID, t.IsPrivate, model.ShareResourceTemplate, t.ID) {
		return nil, fmt.Errorf("templates get: not found")
	}
	return cloneTemplate(t), nil
}

func (s *templateStore) Upsert(_ context.Context, t *model.Template) (*model.Template, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if existing, ok := s.r.templates[t.ID]; ok {
		if existing.UserID != t.UserID {
			return nil, store.ErrNotFound
		}
		t.CreatedAt = existing.CreatedAt
		t.UpdatedAt = time.Now()
		stored := cloneTemplate(t)
		s.r.templates[stored.ID] = stored
		return cloneTemplate(stored), nil
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	stored := cloneTemplate(t)
	s.r.templates[stored.ID] = stored
	return cloneTemplate(stored), nil
}
func (s *templateStore) Create(_ context.Context, t *model.Template) (*model.Template, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	stored := cloneTemplate(t)
	s.r.templates[stored.ID] = stored
	return cloneTemplate(stored), nil
}

func (s *templateStore) Update(_ context.Context, t *model.Template) (*model.Template, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.templates[t.ID]
	if !ok || existing.UserID != t.UserID {
		return nil, fmt.Errorf("templates update: not found")
	}
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = time.Now()
	stored := cloneTemplate(t)
	s.r.templates[stored.ID] = stored
	return cloneTemplate(stored), nil
}

func (s *templateStore) Delete(_ context.Context, id, userID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	t, ok := s.r.templates[id]
	if !ok || t.UserID != userID {
		return nil
	}
	delete(s.r.templates, id)
	return nil
}

func (s *templateStore) Reset() { s.r.Reset() }

// ─── OrgStore ─────────────────────────────────────────────────────────────────

type orgStore struct{ r *Registry }

func (s *orgStore) Create(_ context.Context, org *model.Organization) (*model.Organization, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if org.ID == uuid.Nil {
		org.ID = uuid.New()
	}
	now := time.Now()
	org.CreatedAt = now
	org.UpdatedAt = now
	stored := cloneOrg(org)
	s.r.orgs[stored.ID] = stored
	return cloneOrg(stored), nil
}

func (s *orgStore) GetByID(_ context.Context, id uuid.UUID) (*model.Organization, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	org, ok := s.r.orgs[id]
	if !ok {
		return nil, fmt.Errorf("org get: not found")
	}
	cp := cloneOrg(org)
	for k := range s.r.members {
		if k.orgID == id {
			cp.MemberCount++
		}
	}
	return cp, nil
}

func (s *orgStore) ListForUser(_ context.Context, userID uuid.UUID) ([]*model.OrgWithRole, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.OrgWithRole, 0)
	for k, m := range s.r.members {
		if k.userID != userID {
			continue
		}
		org, ok := s.r.orgs[k.orgID]
		if !ok {
			continue
		}
		cp := cloneOrg(org)
		for mk := range s.r.members {
			if mk.orgID == org.ID {
				cp.MemberCount++
			}
		}
		result = append(result, &model.OrgWithRole{Organization: *cp, Role: m.Role})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (s *orgStore) Update(_ context.Context, org *model.Organization) (*model.Organization, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	existing, ok := s.r.orgs[org.ID]
	if !ok {
		return nil, fmt.Errorf("org update: not found")
	}
	existing.Name = org.Name
	existing.UpdatedAt = time.Now()
	return cloneOrg(existing), nil
}

func (s *orgStore) Delete(_ context.Context, id uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	delete(s.r.orgs, id)
	for k := range s.r.members {
		if k.orgID == id {
			delete(s.r.members, k)
		}
	}
	return nil
}

func (s *orgStore) AddMember(_ context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	k := orgMemberKey{orgID, userID}
	m := &model.OrgMember{OrgID: orgID, UserID: userID, Role: role, JoinedAt: time.Now()}
	if u, ok := s.r.usersByID[userID]; ok {
		m.Email = u.Email
		m.Name = u.Name
	}
	s.r.members[k] = m
	return cloneMember(m), nil
}

func (s *orgStore) GetMember(_ context.Context, orgID, userID uuid.UUID) (*model.OrgMember, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	m, ok := s.r.members[orgMemberKey{orgID, userID}]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneMember(m), nil
}

func (s *orgStore) ListMembers(_ context.Context, orgID uuid.UUID) ([]*model.OrgMember, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.OrgMember, 0)
	for k, m := range s.r.members {
		if k.orgID == orgID {
			result = append(result, cloneMember(m))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].JoinedAt.Before(result[j].JoinedAt) })
	return result, nil
}

func (s *orgStore) UpdateMemberRole(_ context.Context, orgID, userID uuid.UUID, role model.OrgRole) (*model.OrgMember, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	k := orgMemberKey{orgID, userID}
	m, ok := s.r.members[k]
	if !ok {
		return nil, fmt.Errorf("member not found")
	}
	m.Role = role
	return cloneMember(m), nil
}

func (s *orgStore) RemoveMember(_ context.Context, orgID, userID uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	delete(s.r.members, orgMemberKey{orgID, userID})
	return nil
}

func (s *orgStore) GetUserOrgIDs(_ context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	ids := make([]uuid.UUID, 0)
	for k := range s.r.members {
		if k.userID == userID {
			ids = append(ids, k.orgID)
		}
	}
	return ids, nil
}

func (s *orgStore) Reset() { s.r.Reset() }

// ─── ShareStore ───────────────────────────────────────────────────────────────

type shareStore struct{ r *Registry }

func (s *shareStore) Create(_ context.Context, sh *model.Share) (*model.Share, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if sh.ID == uuid.Nil {
		sh.ID = uuid.New()
	}
	sh.CreatedAt = time.Now()
	if u, ok := s.r.usersByID[sh.SharedWith.ID]; ok {
		sh.SharedWith.Email = u.Email
		sh.SharedWith.Name = u.Name
	}
	stored := cloneShare(sh)
	s.r.shares[stored.ID] = stored
	return cloneShare(stored), nil
}

func (s *shareStore) GetByID(_ context.Context, id uuid.UUID) (*model.Share, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	sh, ok := s.r.shares[id]
	if !ok {
		return nil, fmt.Errorf("share get: not found")
	}
	return cloneShare(sh), nil
}

func (s *shareStore) ListForResource(_ context.Context, resourceType model.ShareResourceType, resourceID uuid.UUID) ([]*model.Share, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	result := make([]*model.Share, 0)
	for _, sh := range s.r.shares {
		if sh.ResourceType == resourceType && sh.ResourceID == resourceID {
			result = append(result, cloneShare(sh))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (s *shareStore) GetForUserAndResource(_ context.Context, userID uuid.UUID, resourceType model.ShareResourceType, resourceID uuid.UUID) (*model.Share, error) {
	s.r.mu.RLock()
	defer s.r.mu.RUnlock()
	for _, sh := range s.r.shares {
		if sh.SharedWith.ID == userID && sh.ResourceType == resourceType && sh.ResourceID == resourceID {
			return cloneShare(sh), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *shareStore) UpdatePermission(_ context.Context, id uuid.UUID, permission model.SharePermission) (*model.Share, error) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	sh, ok := s.r.shares[id]
	if !ok {
		return nil, fmt.Errorf("share update: not found")
	}
	sh.Permission = permission
	return cloneShare(sh), nil
}

func (s *shareStore) Delete(_ context.Context, id uuid.UUID) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	delete(s.r.shares, id)
	return nil
}

func (s *shareStore) Reset() { s.r.Reset() }
