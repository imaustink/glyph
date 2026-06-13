package store

import (
	"strings"
	"testing"
)

func TestResourceAccessFilter_ContainsOwnerCheck(t *testing.T) {
	for _, kind := range []ResourceKind{ResourcePage, ResourceTask, ResourceTemplate} {
		sql := ResourceAccessFilter(kind)
		if !strings.Contains(sql, "user_id = $1") {
			t.Errorf("kind %q: missing owner check 'user_id = $1'", kind.table)
		}
	}
}

func TestResourceAccessFilter_ContainsOrgCheck(t *testing.T) {
	for _, kind := range []ResourceKind{ResourcePage, ResourceTask, ResourceTemplate} {
		sql := ResourceAccessFilter(kind)
		if !strings.Contains(sql, "org_members") {
			t.Errorf("kind %q: missing org membership check", kind.table)
		}
		if !strings.Contains(sql, "is_private = false") {
			t.Errorf("kind %q: missing is_private guard", kind.table)
		}
	}
}

func TestResourceAccessFilter_ContainsShareCheck(t *testing.T) {
	for _, kind := range []ResourceKind{ResourcePage, ResourceTask, ResourceTemplate} {
		sql := ResourceAccessFilter(kind)
		if !strings.Contains(sql, "shares") {
			t.Errorf("kind %q: missing shares check", kind.table)
		}
		if !strings.Contains(sql, "shared_with_id = $1") {
			t.Errorf("kind %q: missing shared_with_id check", kind.table)
		}
	}
}

func TestResourceAccessFilter_InterpolatesCorrectValues(t *testing.T) {
	tests := []struct {
		kind             ResourceKind
		wantTable        string
		wantResourceType string
	}{
		{ResourcePage, "pages", "page"},
		{ResourceTask, "tasks", "task"},
		{ResourceTemplate, "templates", "template"},
	}
	for _, tt := range tests {
		sql := ResourceAccessFilter(tt.kind)
		if !strings.Contains(sql, tt.wantTable+".id") {
			t.Errorf("kind %q: expected table reference %q.id in SQL", tt.kind.table, tt.wantTable)
		}
		if !strings.Contains(sql, "'"+tt.wantResourceType+"'") {
			t.Errorf("kind %q: expected resource_type = '%s' in SQL", tt.kind.table, tt.wantResourceType)
		}
	}
}
