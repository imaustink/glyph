package store

import "encoding/json"

// ListItemNodeIDs returns the nodeId of every listItem in a ProseMirror JSON
// document, in document order. The result is never nil, so it can be bound
// directly as a SQL array parameter (a nil slice would bind as NULL, and
// `x = ANY(NULL)` matches nothing — silently skipping reconciliation).
func ListItemNodeIDs(content []byte) ([]string, error) {
	ids := []string{}
	if len(content) == 0 {
		return ids, nil
	}
	var root contentNode
	if err := json.Unmarshal(content, &root); err != nil {
		return ids, err
	}
	var walk func(n *contentNode)
	walk = func(n *contentNode) {
		if n.Type == "listItem" {
			if id, ok := n.Attrs["nodeId"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
		for i := range n.Content {
			walk(&n.Content[i])
		}
	}
	walk(&root)
	return ids, nil
}

type contentNode struct {
	Type    string                 `json:"type"`
	Attrs   map[string]interface{} `json:"attrs"`
	Content []contentNode          `json:"content"`
}
