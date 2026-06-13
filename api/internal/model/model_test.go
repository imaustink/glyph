package model

import "testing"

func TestPriority_IsValid(t *testing.T) {
	valid := []Priority{PriorityUrgent, PriorityHigh, PriorityMedium, PriorityLow, PriorityNone}
	for _, p := range valid {
		if !p.IsValid() {
			t.Errorf("Priority(%q).IsValid() = false, want true", p)
		}
	}
	invalid := []Priority{"", "URGENT", "critical", "0", "none "}
	for _, p := range invalid {
		if p.IsValid() {
			t.Errorf("Priority(%q).IsValid() = true, want false", p)
		}
	}
}

func TestTaskStatus_IsValid(t *testing.T) {
	valid := []TaskStatus{StatusTodo, StatusInProgress, StatusDone, StatusCancelled}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("TaskStatus(%q).IsValid() = false, want true", s)
		}
	}
	invalid := []TaskStatus{"", "TODO", "done ", "pending", "in_progress"}
	for _, s := range invalid {
		if s.IsValid() {
			t.Errorf("TaskStatus(%q).IsValid() = true, want false", s)
		}
	}
}

func TestSharePermission_IsValid(t *testing.T) {
	valid := []SharePermission{SharePermissionViewer, SharePermissionEditor}
	for _, p := range valid {
		if !p.IsValid() {
			t.Errorf("SharePermission(%q).IsValid() = false, want true", p)
		}
	}
	invalid := []SharePermission{"", "owner", "admin", "VIEWER", "editor "}
	for _, p := range invalid {
		if p.IsValid() {
			t.Errorf("SharePermission(%q).IsValid() = true, want false", p)
		}
	}
}

func TestShareResourceType_IsValid(t *testing.T) {
	valid := []ShareResourceType{ShareResourcePage, ShareResourceTask, ShareResourceTemplate}
	for _, rt := range valid {
		if !rt.IsValid() {
			t.Errorf("ShareResourceType(%q).IsValid() = false, want true", rt)
		}
	}
	invalid := []ShareResourceType{"", "PAGE", "note", "task ", "pages"}
	for _, rt := range invalid {
		if rt.IsValid() {
			t.Errorf("ShareResourceType(%q).IsValid() = true, want false", rt)
		}
	}
}
