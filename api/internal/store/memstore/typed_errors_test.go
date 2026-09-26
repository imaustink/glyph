package memstore

import (
	"context"
	"errors"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/store"
	"github.com/google/uuid"
)

// The memstore must return the same sentinel errors as the Postgres store,
// so handlers mapping errors.Is(err, store.ErrNotFound) behave the same on
// both and tests can't pass on one and fail in production.

func TestPageStore_TypedNotFoundErrors(t *testing.T) {
	s := &pageStore{r: NewRegistry()}
	ctx := context.Background()
	owner := uuid.New()
	p, _ := s.Create(ctx, &model.Page{UserID: owner, Type: model.NodeTypePage})

	if _, err := s.GetContent(ctx, p.ID, owner); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetContent(no content) = %v, want ErrNotFound", err)
	}
	if _, err := s.GetContent(ctx, uuid.New(), owner); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetContent(missing page) = %v, want ErrNotFound", err)
	}
	if _, err := s.Update(ctx, &model.Page{ID: uuid.New(), UserID: owner}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Update(missing) = %v, want ErrNotFound", err)
	}
}

func TestShareStore_TypedNotFoundErrors(t *testing.T) {
	s := &shareStore{r: NewRegistry()}
	ctx := context.Background()
	if _, err := s.GetByID(ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetByID(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.UpdatePermission(ctx, uuid.New(), model.SharePermissionEditor); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdatePermission(missing) = %v, want ErrNotFound", err)
	}
}
