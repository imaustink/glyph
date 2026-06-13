package store

import "testing"

func TestDefaultPagination(t *testing.T) {
	tests := []struct {
		name        string
		limit       int
		offset      int
		wantLimit   int
		wantOffset  int
	}{
		{"defaults for zero values", 0, 0, 500, 0},
		{"negative limit uses default", -1, 0, 500, 0},
		{"negative offset clamped to zero", 10, -5, 10, 0},
		{"normal values passed through", 100, 50, 100, 50},
		{"limit at max", 1000, 0, 1000, 0},
		{"limit above max clamped to 1000", 1001, 0, 1000, 0},
		{"large limit clamped", 99999, 0, 1000, 0},
		{"zero offset preserved", 50, 0, 50, 0},
		{"limit of 1 accepted", 1, 0, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := DefaultPagination(tt.limit, tt.offset)
			if pg.Limit != tt.wantLimit {
				t.Errorf("Limit = %d, want %d", pg.Limit, tt.wantLimit)
			}
			if pg.Offset != tt.wantOffset {
				t.Errorf("Offset = %d, want %d", pg.Offset, tt.wantOffset)
			}
		})
	}
}
