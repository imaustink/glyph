package pmmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// TodoTrigger mirrors the page's TodoTriggerConfig: which top-level blocks
// open a TODO section. An empty Pattern means the default (a heading whose
// text is "TODO").
type TodoTrigger struct {
	Pattern    string
	MatchMode  string // "exact" (default) or "regex"
	BlockTypes []string
}

// TodoBullet is a bullet in a TODO section that isn't linked to a task yet.
type TodoBullet struct {
	NodeID  string
	Text    string
	Checked bool
}

// TaskLink is what LinkTodoBullets writes onto a bullet.
type TaskLink struct {
	TaskID string
	Status string
}

// FindUnlinkedTodoBullets ports the editor's TODO detection
// (src/lib/editor/extensions/TodoDetectionExtension.ts) so content written
// outside the editor gets the same treatment: walking the doc's top-level
// blocks, a block of a trigger type opens (if it matches) or closes (if it
// doesn't) a TODO section, and every bullet in a bulletList inside a section
// — including nested bulletLists — that has no taskId is returned. Bullets
// without a nodeId are given one in the returned doc, as the editor does.
func FindUnlinkedTodoBullets(doc json.RawMessage, trigger TodoTrigger) (json.RawMessage, []TodoBullet, error) {
	root, err := decodeTodoDoc(doc)
	if err != nil {
		return nil, nil, err
	}
	match := triggerMatcher(trigger)
	blockTypes := trigger.BlockTypes
	if len(blockTypes) == 0 {
		blockTypes = []string{"heading"}
	}
	isTriggerType := func(t string) bool {
		for _, b := range blockTypes {
			if b == "any" || b == t {
				return true
			}
		}
		return false
	}

	var out []TodoBullet
	inSection := false
	for _, n := range children(root) {
		t, _ := n["type"].(string)
		if isTriggerType(t) {
			inSection = match(strings.TrimSpace(nodeText(n)))
			continue
		}
		if t == "bulletList" && inSection {
			collectUnlinked(n, &out)
		}
	}
	b, err := json.Marshal(root)
	if err != nil {
		return nil, nil, err
	}
	return b, out, nil
}

// LinkTodoBullets sets taskId/taskStatus (and checked for done tasks) on the
// listItems whose nodeId is in links.
func LinkTodoBullets(doc json.RawMessage, links map[string]TaskLink) (json.RawMessage, error) {
	root, err := decodeTodoDoc(doc)
	if err != nil {
		return nil, err
	}
	var walk func(n map[string]interface{})
	walk = func(n map[string]interface{}) {
		if n["type"] == "listItem" {
			attrs, _ := n["attrs"].(map[string]interface{})
			if id, _ := attrs["nodeId"].(string); id != "" {
				if l, ok := links[id]; ok {
					attrs["taskId"] = l.TaskID
					attrs["taskStatus"] = l.Status
					attrs["checked"] = l.Status == "done"
				}
			}
		}
		for _, ch := range children(n) {
			walk(ch)
		}
	}
	walk(root)
	return json.Marshal(root)
}

func decodeTodoDoc(doc json.RawMessage) (map[string]interface{}, error) {
	if len(strings.TrimSpace(string(doc))) == 0 || string(doc) == "null" {
		return map[string]interface{}{"type": "doc", "content": []interface{}{}}, nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("invalid document: %w", err)
	}
	return root, nil
}

func children(n map[string]interface{}) []map[string]interface{} {
	raw, _ := n["content"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, c := range raw {
		if m, ok := c.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func nodeText(n map[string]interface{}) string {
	if t, ok := n["text"].(string); ok {
		return t
	}
	var b strings.Builder
	for _, ch := range children(n) {
		b.WriteString(nodeText(ch))
	}
	return b.String()
}

func triggerMatcher(tr TodoTrigger) func(string) bool {
	pattern := tr.Pattern
	mode := tr.MatchMode
	if strings.TrimSpace(pattern) == "" {
		pattern, mode = "TODO", "exact"
	}
	if mode == "regex" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			// The editor's safeRegexTest also treats an invalid pattern as
			// matching nothing.
			return func(string) bool { return false }
		}
		return re.MatchString
	}
	want := strings.ToLower(strings.TrimSpace(pattern))
	return func(s string) bool { return strings.ToLower(s) == want }
}

func collectUnlinked(list map[string]interface{}, out *[]TodoBullet) {
	for _, item := range children(list) {
		if item["type"] != "listItem" {
			continue
		}
		attrs, _ := item["attrs"].(map[string]interface{})
		if attrs == nil {
			attrs = map[string]interface{}{}
			item["attrs"] = attrs
		}
		nodeID, _ := attrs["nodeId"].(string)
		if nodeID == "" {
			nodeID = uuid.NewString()
			attrs["nodeId"] = nodeID
		}
		if taskID, _ := attrs["taskId"].(string); taskID == "" {
			var text strings.Builder
			for _, ch := range children(item) {
				if ch["type"] == "paragraph" {
					text.WriteString(nodeText(ch))
				}
			}
			checked, _ := attrs["checked"].(bool)
			*out = append(*out, TodoBullet{NodeID: nodeID, Text: strings.TrimSpace(text.String()), Checked: checked})
		}
		for _, ch := range children(item) {
			if ch["type"] == "bulletList" {
				collectUnlinked(ch, out)
			}
		}
	}
}
