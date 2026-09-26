package memstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// Page.Delete must mirror the Postgres store (DI-02 and the memstore
// divergences listed in the audit): ErrNotFound for a page the caller doesn't
// own, a cascade to descendants, refusal when a descendant belongs to someone
// else, and content history that outlives the page.

func TestPageDelete_NotFound(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	if err := s.Delete(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete(missing) = %v, want ErrNotFound", err)
	}

	owner := uuid.New()
	id := uuid.New()
	s.r.pages[id] = &model.Page{ID: id, UserID: owner, Type: model.NodeTypePage}
	if err := s.Delete(context.Background(), id, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete(not owner) = %v, want ErrNotFound", err)
	}
	if _, ok := s.r.pages[id]; !ok {
		t.Fatal("a non-owner delete removed the page")
	}
}

func TestPageDelete_CascadesAndKeepsHistory(t *testing.T) {
	r := NewRegistry()
	s := &pageStore{r: r}
	owner := uuid.New()
	folder, child := uuid.New(), uuid.New()
	now := time.Now()
	r.pages[folder] = &model.Page{ID: folder, UserID: owner, Type: model.NodeTypeFolder, CreatedAt: now}
	r.pages[child] = &model.Page{ID: child, UserID: owner, Type: model.NodeTypePage, ParentID: &folder, CreatedAt: now}
	r.writeContent(child, json.RawMessage(`{"type":"doc","content":[]}`), 1)

	if err := s.Delete(context.Background(), folder, owner); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := r.pages[child]; ok {
		t.Error("descendant page survived its folder's delete")
	}
	if _, ok := r.contents[child]; ok {
		t.Error("descendant content survived its folder's delete")
	}
	if n := len(r.contentVersions[child]); n != 1 {
		t.Errorf("content history after delete has %d versions, want 1 (the archived last content)", n)
	}
}

func TestPageDelete_RefusesSubtreeWithOtherOwners(t *testing.T) {
	r := NewRegistry()
	s := &pageStore{r: r}
	owner, other := uuid.New(), uuid.New()
	folder, theirs := uuid.New(), uuid.New()
	r.pages[folder] = &model.Page{ID: folder, UserID: owner, Type: model.NodeTypeFolder}
	r.pages[theirs] = &model.Page{ID: theirs, UserID: other, Type: model.NodeTypePage, ParentID: &folder}

	err := s.Delete(context.Background(), folder, owner)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Delete = %v, want an ErrConflict", err)
	}
	if _, ok := r.pages[theirs]; !ok {
		t.Fatal("another user's page was deleted")
	}
	if _, ok := r.pages[folder]; !ok {
		t.Fatal("a refused delete removed the folder")
	}
}
