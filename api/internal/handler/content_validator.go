package handler

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Maximum allowed content size: 5 MB, counted in bytes of the JSON as sent.
// The collab service measures the same thing (UTF-8 bytes of the serialised
// document); counting differently lets a document pass one side and be
// refused by the other.
const maxContentSize = 5 * 1024 * 1024

// The allowlists below must equal the editor schema — documentSchema() in
// src/lib/editor/schema.ts (StarterKit v3 + TaskLink). Content the editor
// can't represent loads as a blank editor, and the next keystroke saves that
// blank doc over the note (DI-01); an attribute the editor has but this list
// lacks is stripped on every save (DI-24). TestAllowlistMatchesEditorSchema
// compares them with testdata/editor_schema.json, which a vitest test keeps
// equal to the live schema (`pnpm schema:fixture` regenerates it).

// Allowed ProseMirror node types that can appear in a document.
var allowedNodeTypes = map[string]bool{
	"doc":            true,
	"paragraph":      true,
	"heading":        true,
	"bulletList":     true,
	"orderedList":    true,
	"listItem":       true,
	"text":           true,
	"hardBreak":      true,
	"blockquote":     true,
	"codeBlock":      true,
	"horizontalRule": true,
}

// Allowed attributes per node type. Attributes not in this map are stripped.
var allowedAttrs = map[string]map[string]bool{
	"heading":     {"level": true},
	"orderedList": {"start": true, "type": true},
	"listItem":    {"nodeId": true, "taskId": true, "checked": true, "taskStatus": true},
	"codeBlock":   {"language": true},
}

// Allowed mark types.
var allowedMarkTypes = map[string]bool{
	"bold":      true,
	"italic":    true,
	"strike":    true,
	"code":      true,
	"link":      true,
	"underline": true,
}

// Allowed attributes for marks.
var allowedMarkAttrs = map[string]map[string]bool{
	"link": {"href": true, "target": true, "rel": true, "class": true, "title": true},
}

// urlAttrsByNode / urlAttrsByMark name the attributes whose *values* are URLs
// and therefore need scheme validation, not just key allow-listing.
var urlAttrsByNode = map[string][]string{}

var urlAttrsByMark = map[string][]string{
	"link": {"href"},
}

// isSafeURL reports whether a URL value is safe to store and later render.
//
// Allow-listing the attribute *key* (href/src) said nothing about its value, so
// `javascript:` URLs round-tripped through the API untouched. Notes are shared
// between users, which makes a stored script a user-to-user attack: the victim
// clicks a link in a note someone shared with them and it executes in their
// session. Relative URLs and fragments are permitted; everything with a scheme
// must be http(s) or mailto.
func isSafeURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	// Strip characters that browsers ignore when resolving a scheme, so
	// "java\tscript:" and "  JaVaScript:" cannot slip through.
	normalized := strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r', '\v', '\f', 0:
			return -1
		}
		return r
	}, trimmed)
	normalized = strings.ToLower(normalized)

	// No scheme separator before the first '/', '?' or '#' means it is a
	// relative reference, which cannot carry an executable scheme.
	colon := strings.Index(normalized, ":")
	if colon == -1 {
		return true
	}
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(normalized, sep); i != -1 && i < colon {
			return true
		}
	}
	scheme := normalized[:colon]
	switch scheme {
	case "http", "https", "mailto":
		return true
	default:
		return false
	}
}

// sanitizeURLAttrs drops any allow-listed URL attribute whose value carries an
// unsafe scheme.
func sanitizeURLAttrs(attrs map[string]interface{}, names []string) {
	for _, name := range names {
		v, ok := attrs[name]
		if !ok {
			continue
		}
		str, ok := v.(string)
		if !ok || !isSafeURL(str) {
			delete(attrs, name)
		}
	}
}

// ValidateProseMirrorContent validates and sanitizes ProseMirror JSON content.
// Returns sanitized JSON bytes or an error.
//
// Nodes, marks and attributes the editor schema lacks never reach storage.
// Unknown marks and attributes are dropped. Unknown nodes are downgraded to
// what the editor has rather than refused: a note saved before the allowlist
// matched the schema (an MCP-written image, say) would otherwise be refused
// on every later save, and dropping the node outright would lose its text.
// An image becomes a link to its URL; any other node keeps its text and its
// known children.
func ValidateProseMirrorContent(raw []byte) ([]byte, error) {
	if len(raw) > maxContentSize {
		return nil, fmt.Errorf("content exceeds maximum size of %d bytes", maxContentSize)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	docType, _ := doc["type"].(string)
	if docType != "doc" {
		return nil, fmt.Errorf("invalid document: top-level type must be 'doc', got '%s'", docType)
	}

	sanitizeNode(doc)

	return json.Marshal(doc)
}

// NormalizeStoredContent brings stored content into the editor schema before
// it is served. Content saved before the allowlist matched the schema can
// hold nodes and marks the editor can't build; served as-is it loads as a
// blank editor, and the next keystroke saves that blank doc over the note
// (DI-01). Content that is already valid comes back unchanged in meaning.
// Anything unparseable is returned as-is: the read must not fail over it.
func NormalizeStoredContent(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw
	}
	if t, _ := doc["type"].(string); t != "doc" {
		return raw
	}
	sanitizeNode(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return out
}

// contentKind is what a node's children must be, which decides what an
// unknown child is downgraded to.
type contentKind int

const (
	kindBlock  contentKind = iota // doc, blockquote, listItem
	kindInline                    // paragraph, heading
	kindText                      // codeBlock: plain text only
	kindList                      // bulletList, orderedList: listItems
)

func childKind(parentType string) contentKind {
	switch parentType {
	case "paragraph", "heading":
		return kindInline
	case "codeBlock":
		return kindText
	case "bulletList", "orderedList":
		return kindList
	default:
		return kindBlock
	}
}

func sanitizeNode(node map[string]interface{}) {
	nodeType, _ := node["type"].(string)

	// Strip unknown attributes
	if attrs, ok := node["attrs"].(map[string]interface{}); ok {
		allowed := allowedAttrs[nodeType]
		if allowed == nil {
			delete(node, "attrs")
		} else {
			for key := range attrs {
				if !allowed[key] {
					delete(attrs, key)
				}
			}
			sanitizeURLAttrs(attrs, urlAttrsByNode[nodeType])
			if len(attrs) == 0 {
				delete(node, "attrs")
			}
		}
	}

	sanitizeMarks(node)

	// Recursively sanitize children
	if content, ok := node["content"].([]interface{}); ok {
		kind := childKind(nodeType)
		sanitized := make([]interface{}, 0, len(content))
		for _, child := range content {
			childNode, ok := child.(map[string]interface{})
			if !ok {
				continue
			}
			childType, _ := childNode["type"].(string)
			if !allowedNodeTypes[childType] {
				for _, n := range downgrade(childNode, kind) {
					sanitized = append(sanitized, n)
				}
				continue
			}
			sanitizeNode(childNode)
			sanitized = append(sanitized, childNode)
		}
		node["content"] = sanitized
	}
}

func sanitizeMarks(node map[string]interface{}) {
	marks, ok := node["marks"].([]interface{})
	if !ok {
		return
	}
	sanitized := make([]interface{}, 0, len(marks))
	for _, m := range marks {
		mark, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		markType, _ := mark["type"].(string)
		if !allowedMarkTypes[markType] {
			continue
		}
		// Strip unknown mark attributes
		if markAttrs, hasAttrs := mark["attrs"].(map[string]interface{}); hasAttrs {
			allowed := allowedMarkAttrs[markType]
			if allowed == nil {
				delete(mark, "attrs")
			} else {
				for key := range markAttrs {
					if !allowed[key] {
						delete(markAttrs, key)
					}
				}
				sanitizeURLAttrs(markAttrs, urlAttrsByMark[markType])
				if len(markAttrs) == 0 {
					delete(mark, "attrs")
				}
			}
		}
		sanitized = append(sanitized, mark)
	}
	if len(sanitized) > 0 {
		node["marks"] = sanitized
	} else {
		delete(node, "marks")
	}
}

// downgrade replaces a node the editor schema lacks with sanitized nodes that
// fit where it stood (kind), keeping its text. It may return nothing.
func downgrade(n map[string]interface{}, kind contentKind) []map[string]interface{} {
	switch kind {
	case kindInline:
		return inlineOf(n)
	case kindText:
		var out []map[string]interface{}
		for _, t := range inlineOf(n) {
			if t["type"] == "text" {
				delete(t, "marks")
				out = append(out, t)
			}
		}
		return out
	case kindList:
		blocks := blocksOf(n)
		if len(blocks) == 0 {
			return nil
		}
		if blocks[0]["type"] != "paragraph" {
			blocks = append([]map[string]interface{}{{"type": "paragraph"}}, blocks...)
		}
		return []map[string]interface{}{{"type": "listItem", "content": toAny(blocks)}}
	default:
		return blocksOf(n)
	}
}

// inlineOf flattens an unknown node to sanitized inline nodes.
func inlineOf(n map[string]interface{}) []map[string]interface{} {
	nodeType, _ := n["type"].(string)
	if nodeType == "image" {
		if t := imageLinkText(n); t != nil {
			return []map[string]interface{}{t}
		}
		return nil
	}
	if text, _ := n["text"].(string); text != "" {
		t := map[string]interface{}{"type": "text", "text": text}
		if marks, ok := n["marks"]; ok {
			t["marks"] = marks
		}
		sanitizeMarks(t)
		return []map[string]interface{}{t}
	}
	var out []map[string]interface{}
	for _, c := range childNodes(n) {
		ct, _ := c["type"].(string)
		switch ct {
		case "text", "hardBreak":
			if ct == "text" {
				if s, _ := c["text"].(string); s == "" {
					continue
				}
			}
			sanitizeNode(c)
			out = append(out, c)
		default:
			// Known blocks contribute their text; unknown nodes recurse.
			out = append(out, inlineOf(c)...)
		}
	}
	return out
}

// blocksOf turns an unknown node into sanitized block nodes: its known block
// children are kept, runs of inline content are wrapped in paragraphs, and
// stray list items are wrapped in a bullet list.
func blocksOf(n map[string]interface{}) []map[string]interface{} {
	nodeType, _ := n["type"].(string)
	if nodeType == "image" {
		if t := imageLinkText(n); t != nil {
			return []map[string]interface{}{{"type": "paragraph", "content": []interface{}{t}}}
		}
		return nil
	}
	if text, _ := n["text"].(string); text != "" {
		return []map[string]interface{}{{"type": "paragraph", "content": toAny(inlineOf(n))}}
	}

	var out, inline, items []map[string]interface{}
	flushInline := func() {
		if len(inline) > 0 {
			out = append(out, map[string]interface{}{"type": "paragraph", "content": toAny(inline)})
			inline = nil
		}
	}
	flushItems := func() {
		if len(items) > 0 {
			out = append(out, map[string]interface{}{"type": "bulletList", "content": toAny(items)})
			items = nil
		}
	}
	for _, c := range childNodes(n) {
		ct, _ := c["type"].(string)
		switch {
		case ct == "text" || ct == "hardBreak":
			flushItems()
			inline = append(inline, inlineOf(map[string]interface{}{"content": []interface{}{c}})...)
		case ct == "listItem":
			flushInline()
			sanitizeNode(c)
			items = append(items, c)
		case allowedNodeTypes[ct]:
			flushInline()
			flushItems()
			sanitizeNode(c)
			out = append(out, c)
		default:
			flushInline()
			flushItems()
			out = append(out, blocksOf(c)...)
		}
	}
	flushInline()
	flushItems()
	return out
}

// imageLinkText renders an image as a text node linking to its source (when
// the URL is safe), labelled with its alt text, title or URL.
func imageLinkText(n map[string]interface{}) map[string]interface{} {
	attrs, _ := n["attrs"].(map[string]interface{})
	src, _ := attrs["src"].(string)
	src = strings.TrimSpace(src)
	safe := src != "" && isSafeURL(src)
	label := ""
	for _, key := range []string{"alt", "title"} {
		if s, _ := attrs[key].(string); strings.TrimSpace(s) != "" {
			label = s
			break
		}
	}
	if label == "" && safe {
		label = src
	}
	if label == "" {
		return nil
	}
	t := map[string]interface{}{"type": "text", "text": label}
	if safe {
		t["marks"] = []interface{}{map[string]interface{}{"type": "link", "attrs": map[string]interface{}{"href": src}}}
	}
	return t
}

func childNodes(n map[string]interface{}) []map[string]interface{} {
	raw, _ := n["content"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, c := range raw {
		if m, ok := c.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func toAny(ns []map[string]interface{}) []interface{} {
	out := make([]interface{}, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}
