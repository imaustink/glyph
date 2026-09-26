package store

import (
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

// DI-07: a nil *TodoTriggerConfig must be stored as SQL NULL. Passed through
// an interface{} it is not == nil, so it used to marshal to the JSON literal
// null, which Postgres stores as a JSONB null value.
func TestMarshalNullableJSON_TypedNilIsSQLNull(t *testing.T) {
	var trigger *model.TodoTriggerConfig
	b, err := marshalNullableJSON(trigger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b != nil {
		t.Fatalf("nil trigger marshalled to %q, want nil (SQL NULL)", b)
	}
}

func TestMarshalNullableJSON_TriggerValue(t *testing.T) {
	b, err := marshalNullableJSON(&model.TodoTriggerConfig{Pattern: "TODO"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) == 0 || string(b) == "null" {
		t.Fatalf("got %q, want an encoded config", b)
	}
}

// Rows already holding JSONB null read back as "no trigger", not as an
// empty config (which disables TODO detection).
func TestScanPage_JSONNullTriggerIsNil(t *testing.T) {
	base := pageRowFn(uuid.New(), uuid.New())
	row := &mockRow{scanFn: func(dest ...any) error {
		_ = base(dest...)
		*dest[8].(*[]byte) = []byte("null")
		return nil
	}}
	p, err := scanPage(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.TodoTrigger != nil {
		t.Fatalf("JSONB null todo_trigger scanned as %+v, want nil", *p.TodoTrigger)
	}
}

func TestScanTemplate_JSONNullTriggerIsNil(t *testing.T) {
	base := templateRowFn(uuid.New(), uuid.New())
	row := &mockRow{scanFn: func(dest ...any) error {
		_ = base(dest...)
		*dest[5].(*[]byte) = []byte("null")
		return nil
	}}
	tmpl, err := scanTemplate(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.TodoTrigger != nil {
		t.Fatalf("JSONB null todo_trigger scanned as %+v, want nil", *tmpl.TodoTrigger)
	}
}
