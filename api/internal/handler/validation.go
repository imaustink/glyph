package handler

import (
	"github.com/gin-gonic/gin/binding"
	"github.com/glyph/api/internal/model"
	"github.com/go-playground/validator/v10"
)

// registerValidation is injectable for testing.
var registerValidation = func(v *validator.Validate, tag string, fn validator.Func, callValidationEvenIfNull ...bool) error {
	return v.RegisterValidation(tag, fn, callValidationEvenIfNull...)
}

// RegisterValidators registers custom validators for model enums.
// Call this once at startup before handling requests.
// Panics if validator registration fails (indicates a programming error).
func RegisterValidators() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}

	// Task status enum validation
	if err := registerValidation(v, "taskstatus", func(fl validator.FieldLevel) bool {
		if status, ok := fl.Field().Interface().(model.TaskStatus); ok {
			return status.IsValid()
		}
		// Also accept string values (for when binding from JSON)
		if s, ok := fl.Field().Interface().(string); ok {
			return model.TaskStatus(s).IsValid()
		}
		return false
	}); err != nil {
		panic("failed to register taskstatus validator: " + err.Error())
	}

	// Priority enum validation
	if err := registerValidation(v, "priority", func(fl validator.FieldLevel) bool {
		if p, ok := fl.Field().Interface().(model.Priority); ok {
			return p.IsValid()
		}
		if s, ok := fl.Field().Interface().(string); ok {
			return model.Priority(s).IsValid()
		}
		return false
	}); err != nil {
		panic("failed to register priority validator: " + err.Error())
	}

	// Share permission enum validation
	if err := registerValidation(v, "shareperm", func(fl validator.FieldLevel) bool {
		if p, ok := fl.Field().Interface().(model.SharePermission); ok {
			return p.IsValid()
		}
		if s, ok := fl.Field().Interface().(string); ok {
			return model.SharePermission(s).IsValid()
		}
		return false
	}); err != nil {
		panic("failed to register shareperm validator: " + err.Error())
	}

	// Share resource type enum validation
	if err := registerValidation(v, "sharerestype", func(fl validator.FieldLevel) bool {
		if t, ok := fl.Field().Interface().(model.ShareResourceType); ok {
			return t.IsValid()
		}
		if s, ok := fl.Field().Interface().(string); ok {
			return model.ShareResourceType(s).IsValid()
		}
		return false
	}); err != nil {
		panic("failed to register sharerestype validator: " + err.Error())
	}
}
