package handler

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterValidators(t *testing.T) {
	RegisterValidators()

	v, ok := binding.Validator.Engine().(*validator.Validate)
	require.True(t, ok, "expected *validator.Validate engine")

	// ── taskstatus ────────────────────────────────────────────────────────────
	t.Run("taskstatus_valid", func(t *testing.T) {
		assert.NoError(t, v.Var("todo", "taskstatus"))
		assert.NoError(t, v.Var("in-progress", "taskstatus"))
		assert.NoError(t, v.Var("done", "taskstatus"))
		assert.NoError(t, v.Var("cancelled", "taskstatus"))
	})
	t.Run("taskstatus_invalid", func(t *testing.T) {
		assert.Error(t, v.Var("bogus", "taskstatus"))
		assert.Error(t, v.Var("", "taskstatus"))
	})

	// ── priority ──────────────────────────────────────────────────────────────
	t.Run("priority_valid", func(t *testing.T) {
		assert.NoError(t, v.Var("none", "priority"))
		assert.NoError(t, v.Var("low", "priority"))
		assert.NoError(t, v.Var("medium", "priority"))
		assert.NoError(t, v.Var("high", "priority"))
		assert.NoError(t, v.Var("urgent", "priority"))
	})
	t.Run("priority_invalid", func(t *testing.T) {
		assert.Error(t, v.Var("mega", "priority"))
		assert.Error(t, v.Var("", "priority"))
	})

	// ── shareperm ─────────────────────────────────────────────────────────────
	t.Run("shareperm_valid", func(t *testing.T) {
		assert.NoError(t, v.Var("viewer", "shareperm"))
		assert.NoError(t, v.Var("editor", "shareperm"))
	})
	t.Run("shareperm_invalid", func(t *testing.T) {
		assert.Error(t, v.Var("superuser", "shareperm"))
		assert.Error(t, v.Var("", "shareperm"))
	})

	// ── sharerestype ──────────────────────────────────────────────────────────
	t.Run("sharerestype_valid", func(t *testing.T) {
		assert.NoError(t, v.Var("page", "sharerestype"))
		assert.NoError(t, v.Var("task", "sharerestype"))
		assert.NoError(t, v.Var("template", "sharerestype"))
	})
	t.Run("sharerestype_invalid", func(t *testing.T) {
		assert.Error(t, v.Var("badtype", "sharerestype"))
		assert.Error(t, v.Var("", "sharerestype"))
	})
}

func TestRegisterValidators_NativeTypes(t *testing.T) {
RegisterValidators()

v, ok := binding.Validator.Engine().(*validator.Validate)
require.True(t, ok)

// Pass native model types to hit the first type-assertion branch
t.Run("taskstatus_native", func(t *testing.T) {
assert.NoError(t, v.Var(model.TaskStatus("done"), "taskstatus"))
assert.Error(t, v.Var(model.TaskStatus("bad"), "taskstatus"))
})
t.Run("priority_native", func(t *testing.T) {
assert.NoError(t, v.Var(model.Priority("high"), "priority"))
assert.Error(t, v.Var(model.Priority("mega"), "priority"))
})
t.Run("shareperm_native", func(t *testing.T) {
assert.NoError(t, v.Var(model.SharePermission("viewer"), "shareperm"))
assert.Error(t, v.Var(model.SharePermission("bad"), "shareperm"))
})
t.Run("sharerestype_native", func(t *testing.T) {
assert.NoError(t, v.Var(model.ShareResourceType("page"), "sharerestype"))
assert.Error(t, v.Var(model.ShareResourceType("bad"), "sharerestype"))
})

// Pass incompatible types to hit the final `return false` branch
t.Run("taskstatus_incompatible", func(t *testing.T) {
assert.Error(t, v.Var(42, "taskstatus"))
})
t.Run("priority_incompatible", func(t *testing.T) {
assert.Error(t, v.Var(42, "priority"))
})
t.Run("shareperm_incompatible", func(t *testing.T) {
assert.Error(t, v.Var(42, "shareperm"))
})
t.Run("sharerestype_incompatible", func(t *testing.T) {
assert.Error(t, v.Var(42, "sharerestype"))
})
}
