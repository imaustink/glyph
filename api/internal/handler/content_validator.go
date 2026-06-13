package handler

import (
	"encoding/json"
	"fmt"
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
