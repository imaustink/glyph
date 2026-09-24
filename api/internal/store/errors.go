package store

import "errors"

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
