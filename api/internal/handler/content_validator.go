package handler

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Maximum allowed content size: 5 MB
const maxContentSize = 5 * 1024 * 1024

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
	"image":          true,
}

// Allowed attributes per node type. Attributes not in this map are stripped.
var allowedAttrs = map[string]map[string]bool{
	"heading":   {"level": true},
	"listItem":  {"nodeId": true, "taskId": true, "checked": true, "taskStatus": true},
	"codeBlock": {"language": true},
	"image":     {"src": true, "alt": true, "title": true},
}

// Allowed mark types.
var allowedMarkTypes = map[string]bool{
	"bold":        true,
	"italic":      true,
	"strike":      true,
	"code":        true,
	"link":        true,
	"underline":   true,
	"highlight":   true,
	"subscript":   true,
	"superscript": true,
}

// Allowed attributes for marks.
var allowedMarkAttrs = map[string]map[string]bool{
	"link":      {"href": true, "target": true, "rel": true, "class": true},
	"highlight": {"color": true},
}

// urlAttrsByNode / urlAttrsByMark name the attributes whose *values* are URLs
// and therefore need scheme validation, not just key allow-listing.
var urlAttrsByNode = map[string][]string{
	"image": {"src"},
}

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

	// Sanitize marks
	if marks, ok := node["marks"].([]interface{}); ok {
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

	// Recursively sanitize children
	if content, ok := node["content"].([]interface{}); ok {
		sanitized := make([]interface{}, 0, len(content))
		for _, child := range content {
			childNode, ok := child.(map[string]interface{})
			if !ok {
				continue
			}
			childType, _ := childNode["type"].(string)
			if !allowedNodeTypes[childType] {
				continue // strip unknown node types entirely
			}
			sanitizeNode(childNode)
			sanitized = append(sanitized, childNode)
		}
		node["content"] = sanitized
	}
}
