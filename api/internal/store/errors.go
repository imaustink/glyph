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
