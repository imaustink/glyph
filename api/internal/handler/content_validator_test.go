package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateProseMirrorContent_ValidDocument(t *testing.T) {
	input := []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestValidateProseMirrorContent_ExceedsMaxSize(t *testing.T) {
	raw := make([]byte, maxContentSize+1)
	_, err := ValidateProseMirrorContent(raw)
	if err == nil {
		t.Fatal("expected error for oversized content, got nil")
	}
}

func TestValidateProseMirrorContent_InvalidJSON(t *testing.T) {
	_, err := ValidateProseMirrorContent([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestValidateProseMirrorContent_WrongRootType(t *testing.T) {
	_, err := ValidateProseMirrorContent([]byte(`{"type":"paragraph"}`))
	if err == nil {
		t.Fatal("expected error when root type is not 'doc', got nil")
	}
	if !strings.Contains(err.Error(), "doc") {
		t.Errorf("error message should mention 'doc', got: %v", err)
	}
}

func TestValidateProseMirrorContent_MissingType(t *testing.T) {
	_, err := ValidateProseMirrorContent([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error when type is missing, got nil")
	}
}

func TestValidateProseMirrorContent_StripsUnknownNodeTypes(t *testing.T) {
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [{"type": "text", "text": "keep"}]},
			{"type": "script", "content": []},
			{"type": "iframe", "content": []}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	content, _ := doc["content"].([]interface{})
	if len(content) != 1 {
		t.Errorf("expected 1 child node (script/iframe stripped), got %d", len(content))
	}
}

func TestValidateProseMirrorContent_StripsUnknownAttrs(t *testing.T) {
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "heading", "attrs": {"level": 1, "xss": "<script>"}, "content": [{"type": "text", "text": "hi"}]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	heading, _ := content[0].(map[string]interface{})
	attrs, hasAttrs := heading["attrs"].(map[string]interface{})
	if !hasAttrs {
		t.Fatal("heading should retain its attrs")
	}
	if _, ok := attrs["xss"]; ok {
		t.Error("unknown attr 'xss' should have been stripped")
	}
	if _, ok := attrs["level"]; !ok {
		t.Error("allowed attr 'level' should be preserved")
	}
}

func TestValidateProseMirrorContent_NoAttrsForNonAttrNode(t *testing.T) {
	// paragraph has no allowed attrs — attrs should be stripped entirely
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "attrs": {"class": "bad", "id": "bad"}, "content": [{"type": "text", "text": "x"}]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	para, _ := content[0].(map[string]interface{})
	if _, hasAttrs := para["attrs"]; hasAttrs {
		t.Error("paragraph should have no attrs after sanitisation")
	}
}

func TestValidateProseMirrorContent_StripsUnknownMarks(t *testing.T) {
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "hi", "marks": [
					{"type": "bold"},
					{"type": "xss_mark"},
					{"type": "italic"}
				]}
			]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	para, _ := content[0].(map[string]interface{})
	children, _ := para["content"].([]interface{})
	text, _ := children[0].(map[string]interface{})
	marks, _ := text["marks"].([]interface{})
	if len(marks) != 2 {
		t.Errorf("expected 2 marks (bold + italic), got %d", len(marks))
	}
}

func TestValidateProseMirrorContent_AllMarksStripped_RemovesMarksKey(t *testing.T) {
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "hi", "marks": [{"type": "evil"}]}
			]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	para, _ := content[0].(map[string]interface{})
	children, _ := para["content"].([]interface{})
	text, _ := children[0].(map[string]interface{})
	if _, has := text["marks"]; has {
		t.Error("marks key should be removed when all marks are stripped")
	}
}

func TestValidateProseMirrorContent_StripsUnknownMarkAttrs(t *testing.T) {
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "click", "marks": [
					{"type": "link", "attrs": {"href": "https://example.com", "onclick": "evil()", "target": "_blank"}}
				]}
			]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	para, _ := content[0].(map[string]interface{})
	children, _ := para["content"].([]interface{})
	text, _ := children[0].(map[string]interface{})
	marks, _ := text["marks"].([]interface{})
	link, _ := marks[0].(map[string]interface{})
	attrs, _ := link["attrs"].(map[string]interface{})
	if _, ok := attrs["onclick"]; ok {
		t.Error("disallowed mark attr 'onclick' should be stripped")
	}
	if _, ok := attrs["href"]; !ok {
		t.Error("allowed mark attr 'href' should be preserved")
	}
	if _, ok := attrs["target"]; !ok {
		t.Error("allowed mark attr 'target' should be preserved")
	}
}

func TestValidateProseMirrorContent_ListItemAllowedAttrs(t *testing.T) {
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "bulletList", "content": [
				{"type": "listItem", "attrs": {"nodeId": "abc123", "taskId": "task1", "checked": true, "evil": "xss"}, "content": []}
			]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	docContent, _ := doc["content"].([]interface{})
	list, _ := docContent[0].(map[string]interface{})
	listContent, _ := list["content"].([]interface{})
	item, _ := listContent[0].(map[string]interface{})
	attrs, _ := item["attrs"].(map[string]interface{})
	if _, ok := attrs["evil"]; ok {
		t.Error("disallowed listItem attr 'evil' should be stripped")
	}
	for _, allowed := range []string{"nodeId", "taskId", "checked"} {
		if _, ok := attrs[allowed]; !ok {
			t.Errorf("allowed listItem attr %q should be preserved", allowed)
		}
	}
}

func TestValidateProseMirrorContent_AllAttrsStripped_RemovesAttrsKey(t *testing.T) {
	// heading with ONLY a disallowed attr → after stripping, attrs is empty → attrs key deleted
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "heading", "attrs": {"evil": "xss"}, "content": [{"type": "text", "text": "hi"}]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	heading, _ := content[0].(map[string]interface{})
	if _, hasAttrs := heading["attrs"]; hasAttrs {
		t.Error("attrs key should be removed when all attrs are stripped")
	}
}

func TestValidateProseMirrorContent_MarkWithAttrsButNoAllowedAttrs(t *testing.T) {
	// bold mark has no entry in allowedMarkAttrs → its attrs should be deleted entirely
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "hi", "marks": [
					{"type": "bold", "attrs": {"color": "red", "class": "custom"}}
				]}
			]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	para, _ := content[0].(map[string]interface{})
	children, _ := para["content"].([]interface{})
	text, _ := children[0].(map[string]interface{})
	marks, _ := text["marks"].([]interface{})
	bold, _ := marks[0].(map[string]interface{})
	if _, hasAttrs := bold["attrs"]; hasAttrs {
		t.Error("bold mark attrs should be removed (bold has no allowed attrs)")
	}
}

func TestValidateProseMirrorContent_MarkAllowedAttrsAllStripped_RemovesAttrsKey(t *testing.T) {
	// link mark with only disallowed attrs → after stripping, attrs empty → attrs key deleted
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "click", "marks": [
					{"type": "link", "attrs": {"onclick": "evil()", "data-evil": "bad"}}
				]}
			]}
		]
	}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(out, &doc)
	content, _ := doc["content"].([]interface{})
	para, _ := content[0].(map[string]interface{})
	children, _ := para["content"].([]interface{})
	text, _ := children[0].(map[string]interface{})
	marks, _ := text["marks"].([]interface{})
	link, _ := marks[0].(map[string]interface{})
	if _, hasAttrs := link["attrs"]; hasAttrs {
		t.Error("link attrs key should be removed when all attrs are disallowed")
	}
}

func TestValidateProseMirrorContent_EmptyDocument(t *testing.T) {
	input := []byte(`{"type":"doc","content":[]}`)
	out, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), `"type":"doc"`) {
		t.Error("output should contain doc type")
	}
}

func TestValidateProseMirrorContent_DeepNesting(t *testing.T) {
	// Blockquote > bulletList > listItem > paragraph > text — all valid
	input := []byte(`{
		"type": "doc",
		"content": [
			{"type": "blockquote", "content": [
				{"type": "bulletList", "content": [
					{"type": "listItem", "content": [
						{"type": "paragraph", "content": [
							{"type": "text", "text": "deep"}
						]}
					]}
				]}
			]}
		]
	}`)
	_, err := ValidateProseMirrorContent(input)
	if err != nil {
		t.Fatalf("unexpected error for valid deeply nested doc: %v", err)
	}
}
