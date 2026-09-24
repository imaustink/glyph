// Package pmmd converts between Glyph's stored ProseMirror/TipTap JSON
// documents and Markdown.
//
// It is deliberately dependency-free (standard library plus google/uuid) and
// implements a pragmatic CommonMark + GFM subset: exactly what is needed for
// AI agents to read and write notes as Markdown without losing structure the
// editor relies on (list-item nodeIds, task links).
//
// Task-linked bullets are rendered as GFM task items carrying a trailing
// `<!-- task:UUID -->` comment so the link survives a Markdown round trip.
package pmmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// node is a ProseMirror JSON node. Field order matches TipTap's getJSON().
type node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []*node        `json:"content,omitempty"`
	Marks   []mark         `json:"marks,omitempty"`
	Text    string         `json:"text,omitempty"`
}

type mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// ToMarkdown renders a ProseMirror JSON doc (the raw JSONB bytes) as Markdown.
// An empty or null document renders as the empty string.
func ToMarkdown(doc json.RawMessage) (string, error) {
	if isEmptyJSON(doc) {
		return "", nil
	}
	var root node
	if err := json.Unmarshal(doc, &root); err != nil {
		return "", fmt.Errorf("pmmd: invalid document: %w", err)
	}
	blocks := root.Content
	if root.Type != "doc" {
		blocks = []*node{&root}
	}
	s := strings.TrimRight(strings.Join(renderBlocks(blocks, false), "\n"), "\n")
	if s == "" {
		return "", nil
	}
	return s + "\n", nil
}

// FromMarkdown parses Markdown into a ProseMirror JSON doc
// ({"type":"doc","content":[...]}). Empty input yields a doc containing a
// single empty paragraph, which is what an empty TipTap editor produces.
func FromMarkdown(md string) (json.RawMessage, error) {
	blocks := parseMarkdown(md)
	if len(blocks) == 0 {
		blocks = []*node{{Type: "paragraph"}}
	}
	return json.Marshal(&node{Type: "doc", Content: blocks})
}

// AppendMarkdown returns existing with the blocks parsed from md appended at
// the end. existing may be empty/null (treated as an empty doc). Existing
// nodes are carried over as raw JSON and never round-tripped through Markdown.
// If existing consists solely of a single empty paragraph (an empty TipTap
// doc) that placeholder is replaced rather than kept in front of the new
// content.
func AppendMarkdown(existing json.RawMessage, md string) (json.RawMessage, error) {
	top, content, err := splitDoc(existing)
	if err != nil {
		return nil, err
	}
	blocks := parseMarkdown(md)
	if len(blocks) > 0 && isPlaceholderContent(content) {
		content = nil
	}
	for _, b := range blocks {
		raw, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		content = append(content, raw)
	}
	return joinDoc(top, content)
}

// PreserveTaskLinks: for every listItem in next that has a taskId, if prev
// contains a listItem with the same taskId, copy that item's nodeId and
// taskStatus onto it. Only the first occurrence of a taskId in next is
// updated so a duplicated bullet cannot produce duplicate nodeIds. Returns
// the updated next.
func PreserveTaskLinks(prev, next json.RawMessage) (json.RawMessage, error) {
	if isEmptyJSON(next) || isEmptyJSON(prev) {
		return next, nil
	}
	prevDoc, err := decodeGeneric(prev)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid prev document: %w", err)
	}
	nextDoc, err := decodeGeneric(next)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid next document: %w", err)
	}

	type link struct {
		nodeID, status   any
		hasNode, hasStat bool
	}
	links := map[string]link{}
	walkGeneric(prevDoc, func(m map[string]any) {
		if m["type"] != "listItem" {
			return
		}
		attrs, _ := m["attrs"].(map[string]any)
		id, _ := attrs["taskId"].(string)
		if id == "" {
			return
		}
		if _, seen := links[id]; seen {
			return
		}
		var l link
		if v, ok := attrs["nodeId"]; ok && v != nil {
			l.nodeID, l.hasNode = v, true
		}
		if v, ok := attrs["taskStatus"]; ok && v != nil {
			l.status, l.hasStat = v, true
		}
		links[id] = l
	})
	if len(links) == 0 {
		return next, nil
	}

	used := map[string]bool{}
	walkGeneric(nextDoc, func(m map[string]any) {
		if m["type"] != "listItem" {
			return
		}
		attrs, _ := m["attrs"].(map[string]any)
		id, _ := attrs["taskId"].(string)
		if id == "" || used[id] {
			return
		}
		l, ok := links[id]
		if !ok {
			return
		}
		used[id] = true
		if l.hasNode {
			attrs["nodeId"] = l.nodeID
		}
		if l.hasStat {
			attrs["taskStatus"] = l.status
			// Keep `checked` consistent with the restored status so the stored
			// node can't contradict itself (e.g. taskStatus:"done" with
			// checked:false) after a reverted markdown checkbox edit.
			if s, ok := l.status.(string); ok {
				attrs["checked"] = s == "done" || s == "cancelled"
			}
		}
	})
	return json.Marshal(nextDoc)
}

// AppendTaskBullet adds a task-linked bullet under the first heading whose
// plain text equals todoHeading (case-insensitive, default "TODO"). If the
// block right after that heading is a bulletList the item is appended to it;
// otherwise a new bulletList is inserted right after the heading. If no such
// heading exists, a level-2 heading with the todoHeading text plus a new
// bulletList are appended at the end of the doc. An empty nodeID gets a fresh
// UUID; an empty taskID or taskStatus is omitted.
func AppendTaskBullet(existing json.RawMessage, text, nodeID, taskID, taskStatus, todoHeading string) (json.RawMessage, error) {
	if strings.TrimSpace(todoHeading) == "" {
		todoHeading = "TODO"
	}
	if nodeID == "" {
		nodeID = uuid.NewString()
	}
	top, content, err := splitDoc(existing)
	if err != nil {
		return nil, err
	}

	attrs := map[string]any{"nodeId": nodeID, "checked": taskStatus == "done"}
	if taskID != "" {
		attrs["taskId"] = taskID
	}
	if taskStatus != "" {
		attrs["taskStatus"] = taskStatus
	}
	para := &node{Type: "paragraph"}
	if text != "" {
		para.Content = []*node{{Type: "text", Text: text}}
	}
	item := &node{Type: "listItem", Attrs: attrs, Content: []*node{para}}
	itemRaw, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	newList, err := json.Marshal(&node{Type: "bulletList", Content: []*node{item}})
	if err != nil {
		return nil, err
	}

	want := normalizeHeadingText(todoHeading)
	for i, raw := range content {
		var h node
		if json.Unmarshal(raw, &h) != nil || h.Type != "heading" {
			continue
		}
		if normalizeHeadingText(textContent(&h)) != want {
			continue
		}
		if i+1 < len(content) {
			var next map[string]json.RawMessage
			if json.Unmarshal(content[i+1], &next) == nil && rawString(next["type"]) == "bulletList" {
				var items []json.RawMessage
				if c, ok := next["content"]; ok && !isEmptyJSON(c) {
					if err := json.Unmarshal(c, &items); err != nil {
						return nil, fmt.Errorf("pmmd: invalid bulletList content: %w", err)
					}
				}
				items = append(items, itemRaw)
				if next["content"], err = json.Marshal(items); err != nil {
					return nil, err
				}
				if content[i+1], err = json.Marshal(next); err != nil {
					return nil, err
				}
				return joinDoc(top, content)
			}
		}
		out := make([]json.RawMessage, 0, len(content)+1)
		out = append(out, content[:i+1]...)
		out = append(out, newList)
		out = append(out, content[i+1:]...)
		return joinDoc(top, out)
	}

	heading, err := json.Marshal(&node{
		Type:    "heading",
		Attrs:   map[string]any{"level": 2},
		Content: []*node{{Type: "text", Text: strings.TrimSpace(todoHeading)}},
	})
	if err != nil {
		return nil, err
	}
	if isPlaceholderContent(content) {
		content = nil
	}
	content = append(content, heading, newList)
	return joinDoc(top, content)
}

func normalizeHeadingText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ":")
	return strings.ToLower(strings.TrimSpace(s))
}

// ---- raw document helpers ----

func isEmptyJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

func rawString(b json.RawMessage) string {
	var s string
	_ = json.Unmarshal(b, &s)
	return s
}

// splitDoc decodes the top level of a doc, keeping every child as raw JSON.
func splitDoc(doc json.RawMessage) (map[string]json.RawMessage, []json.RawMessage, error) {
	top := map[string]json.RawMessage{}
	if isEmptyJSON(doc) {
		top["type"] = json.RawMessage(`"doc"`)
		return top, nil, nil
	}
	if err := json.Unmarshal(doc, &top); err != nil {
		return nil, nil, fmt.Errorf("pmmd: invalid document: %w", err)
	}
	if t, ok := top["type"]; !ok {
		top["type"] = json.RawMessage(`"doc"`)
	} else if rawString(t) != "doc" {
		return nil, nil, errors.New("pmmd: invalid document: top-level type must be 'doc'")
	}
	var content []json.RawMessage
	if c, ok := top["content"]; ok && !isEmptyJSON(c) {
		if err := json.Unmarshal(c, &content); err != nil {
			return nil, nil, fmt.Errorf("pmmd: invalid document content: %w", err)
		}
	}
	return top, content, nil
}

func joinDoc(top map[string]json.RawMessage, content []json.RawMessage) (json.RawMessage, error) {
	if len(content) == 0 {
		content = []json.RawMessage{json.RawMessage(`{"type":"paragraph"}`)}
	}
	c, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	top["content"] = c
	return json.Marshal(top)
}

// isPlaceholderContent reports whether content is exactly one empty
// paragraph, i.e. a freshly created TipTap document.
func isPlaceholderContent(content []json.RawMessage) bool {
	if len(content) != 1 {
		return false
	}
	var p node
	if json.Unmarshal(content[0], &p) != nil {
		return false
	}
	return p.Type == "paragraph" && len(p.Content) == 0
}

func decodeGeneric(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func walkGeneric(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		if c, ok := t["content"].([]any); ok {
			for _, child := range c {
				walkGeneric(child, fn)
			}
		}
	case []any:
		for _, child := range t {
			walkGeneric(child, fn)
		}
	}
}

// textContent returns the plain text of a node, separating block children
// with a space.
func textContent(n *node) string {
	if n == nil {
		return ""
	}
	switch n.Type {
	case "text":
		return n.Text
	case "hardBreak":
		return " "
	}
	var b strings.Builder
	for i, c := range n.Content {
		if i > 0 && !isInlineType(c.Type) && b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(textContent(c))
	}
	return b.String()
}

func isInlineType(t string) bool { return t == "text" || t == "hardBreak" }

// ---- marks ----

var markRank = map[string]int{"link": 0, "bold": 1, "italic": 2, "strike": 3, "code": 4}

func markEqual(a, b mark) bool {
	if a.Type != b.Type {
		return false
	}
	if a.Type == "link" {
		return linkHref(a) == linkHref(b)
	}
	return true
}

func linkHref(m mark) string {
	s, _ := m.Attrs["href"].(string)
	return s
}

func hasMark(ms []mark, m mark) bool {
	for _, x := range ms {
		if markEqual(x, m) {
			return true
		}
	}
	return false
}

func hasMarkType(ms []mark, t string) bool {
	for _, x := range ms {
		if x.Type == t {
			return true
		}
	}
	return false
}

func sameMarks(a, b []mark) bool {
	if len(a) != len(b) {
		return false
	}
	for _, m := range a {
		if !hasMark(b, m) {
			return false
		}
	}
	return true
}

func sortMarks(ms []mark) {
	sort.SliceStable(ms, func(i, j int) bool { return rankOf(ms[i].Type) < rankOf(ms[j].Type) })
}

func rankOf(t string) int {
	if r, ok := markRank[t]; ok {
		return r
	}
	return 99
}

// isSafeURL mirrors handler.isSafeURL: relative references and http, https
// and mailto URLs are allowed; any other scheme is rejected.
func isSafeURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	normalized := strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r', '\v', '\f', 0:
			return -1
		}
		return r
	}, trimmed)
	normalized = strings.ToLower(normalized)
	colon := strings.Index(normalized, ":")
	if colon == -1 {
		return true
	}
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(normalized, sep); i != -1 && i < colon {
			return true
		}
	}
	switch normalized[:colon] {
	case "http", "https", "mailto":
		return true
	}
	return false
}
