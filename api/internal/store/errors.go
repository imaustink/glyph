package store

import (
	"errors"
	"fmt"
)

// Sentinel errors for consistent error handling across the store layer.
// Use errors.Is() to check for these in handlers.

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrForbidden is returned when access to a resource is denied.
var ErrForbidden = errors.New("forbidden")

// ErrConflict is returned when an operation would violate a constraint.
var ErrConflict = errors.New("conflict")

// ErrCollaborative is returned for a whole-document content write to a page
// whose content is owned by a live collaborative session. Such writes must go
// through the collab service instead.
var ErrCollaborative = errors.New("page is being edited collaboratively")

// ErrStaleSnapshot is returned when a collaborative snapshot was produced for
// an epoch that is no longer current, or would move the snapshot backwards.
var ErrStaleSnapshot = errors.New("stale collaborative snapshot")

// ErrSubtreeNotOwned is returned when deleting a page or folder whose subtree
// contains pages owned by someone other than the caller. The parent_id
// cascade would otherwise destroy other users' work (DI-02).
var ErrSubtreeNotOwned = fmt.Errorf("%w: the folder contains pages owned by other users", ErrConflict)

// ErrTypeImmutable is returned when a write would change a page's type
// (page↔folder). Shares are typed, so a changed type left them unmanageable.
var ErrTypeImmutable = errors.New("a page's type cannot be changed")

// ErrCycle is returned when a write would make a page its own ancestor
// (parent_id pointing at itself or at one of its descendants).
var ErrCycle = errors.New("a node cannot be moved under itself or one of its descendants")

// ErrLastOwner is returned when a membership change would leave an
// organization with no owner.
var ErrLastOwner = errors.New("organization must keep at least one owner")

// ErrTaskLive is returned when moving a task onto another bullet while its
// own bullet is still on its note: the bullet was copied, or it was cut and
// the note's save without it hasn't reached the server yet.
var ErrTaskLive = errors.New("the task's bullet is still on its note")

// ErrTaskNotMovable is returned when moving a task that can't follow a
// bullet: one the user deleted, or one that was never a bullet's task.
var ErrTaskNotMovable = errors.New("the task can't be moved to another bullet")
