package handler

import (
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

func TestUpdatePageRequest_ApplyTo(t *testing.T) {
	original := &model.Page{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		Type:      model.NodeTypePage,
		Title:     "Original Title",
		ParentID:  nil,
		Order:     0,
		Tags:      []string{"old"},
		IsPrivate: true,
	}

	newTitle := "Updated Title"
	newOrder := 5
	newTags := []string{"new", "tags"}
	req := UpdatePageRequest{
		Title: &newTitle,
		Order: &newOrder,
		Tags:  &newTags,
	}

	req.ApplyTo(original)

	if original.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", original.Title)
	}
	if original.Order != 5 {
		t.Errorf("expected order 5, got %d", original.Order)
	}
	if len(original.Tags) != 2 || original.Tags[0] != "new" {
		t.Errorf("expected tags [new tags], got %v", original.Tags)
	}
	// Fields not in the request should be unchanged
	if original.Type != model.NodeTypePage {
		t.Errorf("type should be unchanged, got %q", original.Type)
	}
	if original.IsPrivate != true {
		t.Error("isPrivate should be unchanged")
	}
}

func TestUpdatePageRequest_DoesNotOverwriteIDOrUserID(t *testing.T) {
	originalID := uuid.New()
	originalUserID := uuid.New()
	page := &model.Page{
		ID:     originalID,
		UserID: originalUserID,
		Title:  "Test",
	}

	title := "New Title"
	req := UpdatePageRequest{Title: &title}
	req.ApplyTo(page)

	if page.ID != originalID {
		t.Error("ID should not be modifiable via DTO")
	}
	if page.UserID != originalUserID {
		t.Error("UserID should not be modifiable via DTO")
	}
}

func TestUpdateTaskRequest_ApplyTo(t *testing.T) {
	task := &model.Task{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Title:    "Original",
		Status:   model.StatusTodo,
		Priority: model.PriorityNone,
		Tags:     []string{},
		Order:    0,
	}

	newTitle := "Updated"
	newStatus := model.StatusInProgress
	newPriority := model.PriorityHigh
	newOrder := 10
	req := UpdateTaskRequest{
		Title:    &newTitle,
		Status:   &newStatus,
		Priority: &newPriority,
		Order:    &newOrder,
	}

	req.ApplyTo(task)

	if task.Title != "Updated" {
		t.Errorf("expected title 'Updated', got %q", task.Title)
	}
	if task.Status != model.StatusInProgress {
		t.Errorf("expected status 'in-progress', got %q", task.Status)
	}
	if task.Priority != model.PriorityHigh {
		t.Errorf("expected priority 'high', got %q", task.Priority)
	}
	if task.Order != 10 {
		t.Errorf("expected order 10, got %d", task.Order)
	}
}

func TestUpdateLaneRequest_ApplyTo(t *testing.T) {
	lane := &model.Lane{
		ID:     uuid.New(),
		UserID: uuid.New(),
		Title:  "All Tasks",
		FilterSet: model.FilterSet{
			Conjunction: model.ConjunctionAnd,
			Rules:       []model.FilterRule{},
		},
		SortConfig: model.SortConfig{Mode: model.SortModeAuto},
		Order:      0,
	}

	newTitle := "In Progress"
	newOrder := 2
	req := UpdateLaneRequest{
		Title: &newTitle,
		Order: &newOrder,
	}

	req.ApplyTo(lane)

	if lane.Title != "In Progress" {
		t.Errorf("expected title 'In Progress', got %q", lane.Title)
	}
	if lane.Order != 2 {
		t.Errorf("expected order 2, got %d", lane.Order)
	}
	// FilterSet should be unchanged when not in request
	if lane.FilterSet.Conjunction != model.ConjunctionAnd {
		t.Error("filterSet should be unchanged")
	}
}

func TestUpdateTemplateRequest_ApplyTo(t *testing.T) {
	tmpl := &model.Template{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		Name:      "Default",
		Content:   "{}",
		IsDefault: true,
	}

	newName := "Weekly Review"
	isDefault := false
	req := UpdateTemplateRequest{
		Name:      &newName,
		IsDefault: &isDefault,
	}

	req.ApplyTo(tmpl)

	if tmpl.Name != "Weekly Review" {
		t.Errorf("expected name 'Weekly Review', got %q", tmpl.Name)
	}
	if tmpl.IsDefault != false {
		t.Error("expected isDefault false")
	}
	// Content should be unchanged
	if tmpl.Content != "{}" {
		t.Errorf("content should be unchanged, got %q", tmpl.Content)
	}
}

func TestUpdateTaskRequest_ApplyTo_AllFields(t *testing.T) {
task := &model.Task{
ID:     uuid.New(),
UserID: uuid.New(),
Title:  "Original",
}

desc := "Full description"
tags := []string{"work", "q4"}
due := "2024-12-31"
srcPageID := uuid.New()
srcNodeID := "node-abc"
linkTitle := "Example"
link := &model.LinkMeta{URL: "https://example.com", Title: &linkTitle}
orgID := uuid.New()
isPrivate := true

req := UpdateTaskRequest{
Description:  &desc,
Tags:         &tags,
DueDate:      &due,
SourcePageID: &srcPageID,
SourceNodeID: &srcNodeID,
Link:         link,
OrgID:        &orgID,
IsPrivate:    &isPrivate,
}

req.ApplyTo(task)

if task.Description != desc {
t.Errorf("Description = %q, want %q", task.Description, desc)
}
if len(task.Tags) != 2 || task.Tags[0] != "work" {
t.Errorf("Tags = %v", task.Tags)
}
if task.DueDate == nil || *task.DueDate != due {
t.Errorf("DueDate = %v, want %q", task.DueDate, due)
}
if task.SourcePageID == nil || *task.SourcePageID != srcPageID {
t.Errorf("SourcePageID = %v, want %v", task.SourcePageID, srcPageID)
}
if task.SourceNodeID == nil || *task.SourceNodeID != srcNodeID {
t.Errorf("SourceNodeID = %v, want %q", task.SourceNodeID, srcNodeID)
}
if task.Link == nil || task.Link.URL != "https://example.com" {
t.Errorf("Link = %v", task.Link)
}
if task.OrgID == nil || *task.OrgID != orgID {
t.Errorf("OrgID = %v, want %v", task.OrgID, orgID)
}
if !task.IsPrivate {
t.Error("IsPrivate should be true")
}
}

func TestUpdateLaneRequest_ApplyTo_AllFields(t *testing.T) {
lane := &model.Lane{
ID:         uuid.New(),
UserID:     uuid.New(),
Title:      "Old Title",
FilterSet:  model.FilterSet{Conjunction: model.ConjunctionAnd, Rules: []model.FilterRule{}},
SortConfig: model.SortConfig{Mode: model.SortModeAuto},
}

newFilterSet := model.FilterSet{
Conjunction: model.ConjunctionOr,
Rules:       []model.FilterRule{{Field: "status", Operator: model.FilterOpEq, Value: "done"}},
}
dir := model.SortDirectionDesc
field := "priority"
newSortConfig := model.SortConfig{Mode: model.SortModeField, Field: &field, Direction: &dir}

req := UpdateLaneRequest{FilterSet: &newFilterSet, SortConfig: &newSortConfig}
req.ApplyTo(lane)

if lane.FilterSet.Conjunction != model.ConjunctionOr {
t.Errorf("FilterSet.Conjunction = %q, want 'or'", lane.FilterSet.Conjunction)
}
if len(lane.FilterSet.Rules) != 1 {
t.Errorf("FilterSet.Rules len = %d, want 1", len(lane.FilterSet.Rules))
}
if lane.SortConfig.Mode != model.SortModeField {
t.Errorf("SortConfig.Mode = %q, want 'field'", lane.SortConfig.Mode)
}
}

func TestUpdateTemplateRequest_ApplyTo_AllFields(t *testing.T) {
tmpl := &model.Template{ID: uuid.New(), UserID: uuid.New(), Name: "Old"}

content := "## New Content"
titleTpl := "Weekly: {{date}}"
todoTrigger := &model.TodoTriggerConfig{Pattern: "TODO", BlockTypes: []string{"bulletList"}}
folderID := uuid.New()
orgID := uuid.New()
isPrivate := true

req := UpdateTemplateRequest{
Content:         &content,
TitleTemplate:   &titleTpl,
TodoTrigger:     todoTrigger,
DefaultFolderID: &folderID,
OrgID:           &orgID,
IsPrivate:       &isPrivate,
}
req.ApplyTo(tmpl)

if tmpl.Content != content {
t.Errorf("Content = %q, want %q", tmpl.Content, content)
}
if tmpl.TitleTemplate != titleTpl {
t.Errorf("TitleTemplate = %q, want %q", tmpl.TitleTemplate, titleTpl)
}
if tmpl.TodoTrigger == nil || tmpl.TodoTrigger.Pattern != "TODO" {
t.Errorf("TodoTrigger = %v", tmpl.TodoTrigger)
}
if tmpl.DefaultFolderID == nil || *tmpl.DefaultFolderID != folderID {
t.Errorf("DefaultFolderID = %v, want %v", tmpl.DefaultFolderID, folderID)
}
if tmpl.OrgID == nil || *tmpl.OrgID != orgID {
t.Errorf("OrgID = %v, want %v", tmpl.OrgID, orgID)
}
if !tmpl.IsPrivate {
t.Error("IsPrivate should be true")
}
}

func TestUpdatePageRequest_ApplyTo_AllFields(t *testing.T) {
page := &model.Page{ID: uuid.New(), UserID: uuid.New(), Title: "Old"}

pageType := model.NodeTypeFolder
parentID := uuid.New()
todoTrigger := &model.TodoTriggerConfig{Pattern: "ACTION"}
orgID := uuid.New()
isPrivate := true

req := UpdatePageRequest{
Type:        &pageType,
ParentID:    &parentID,
TodoTrigger: todoTrigger,
OrgID:       &orgID,
IsPrivate:   &isPrivate,
}
req.ApplyTo(page)

if page.Type != model.NodeTypeFolder {
t.Errorf("Type = %q, want 'folder'", page.Type)
}
if page.ParentID == nil || *page.ParentID != parentID {
t.Errorf("ParentID = %v, want %v", page.ParentID, parentID)
}
if page.TodoTrigger == nil || page.TodoTrigger.Pattern != "ACTION" {
t.Errorf("TodoTrigger = %v", page.TodoTrigger)
}
if page.OrgID == nil || *page.OrgID != orgID {
t.Errorf("OrgID = %v, want %v", page.OrgID, orgID)
}
if !page.IsPrivate {
t.Error("IsPrivate should be true")
}
}
