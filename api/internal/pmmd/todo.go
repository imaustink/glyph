package pmmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/google/uuid"
)

// TodoTrigger mirrors the page's TodoTriggerConfig: which top-level blocks
// open a TODO section. An empty Pattern means the default (a block whose
// text is "TODO", exact); BlockTypes still apply.
//
// The editor derives TODO bullets the same way
// (src/lib/editor/todoDerivation.ts). Both are tested against
// testdata/todo_derivation.json; change them together.
type TodoTrigger struct {
	Pattern    string
	MatchMode  string // "exact" (default) or "regex"
	BlockTypes []string
}

// TodoBullet is a bullet in a TODO section that isn't linked to a task yet
// and has text (an empty bullet is not a task).
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
// — including nested bulletLists — that has no taskId and some text is
// returned. Bullets without a nodeId are given one in the returned doc, as
// the editor does.
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
		// A list is never a trigger block, not even with "any".
		isList := t == "bulletList" || t == "orderedList"
		for _, b := range blockTypes {
			if b == t || (b == "any" && !isList) {
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

// LinkTodoBullets sets taskId/taskStatus (and checked for done or cancelled
// tasks, as the editor shows them) on the listItems whose nodeId is in links.
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
					attrs["checked"] = l.Status == "done" || l.Status == "cancelled"
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
		if !portableRegex(pattern) {
			return func(string) bool { return false }
		}
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

// maxTriggerPatternLen and nestedQuantifier mirror the editor's
// safeRegex.ts, which refuses such patterns (ReDoS guard).
const maxTriggerPatternLen = 200

var nestedQuantifier = regexp.MustCompile(`([+*{][?]?[)]\s*[+*{])|([+*{][?]?\s*[+*{])`)

// portableRegex reports whether a trigger pattern means the same in the
// editor (JavaScript) and here (RE2). Patterns the editor refuses, and
// constructs only RE2 supports — flag groups like (?i), (?P<…>), \A, \z,
// \p{…}, \x{…}, \Q…\E, POSIX [[:class:]] — match nothing, as in the
// editor. (The editor refuses the JS-only constructs.)
func portableRegex(pattern string) bool {
	// Length in UTF-16 units, as JavaScript counts it.
	if len(utf16.Encode([]rune(pattern))) > maxTriggerPatternLen || nestedQuantifier.MatchString(pattern) {
		return false
	}
	if strings.Contains(pattern, "[[:") {
		return false
	}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				switch pattern[i+1] {
				case 'A', 'z', 'C', 'Q', 'E', 'p', 'P':
					return false
				case 'x':
					if i+2 < len(pattern) && pattern[i+2] == '{' {
						return false
					}
				}
			}
			i++
		case '(':
			if i+2 < len(pattern) && pattern[i+1] == '?' {
				next := pattern[i+2]
				named := next == '<' && i+3 < len(pattern) && isASCIILetter(pattern[i+3])
				if next != ':' && !named {
					return false
				}
			}
		}
	}
	return true
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
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
			if t := strings.TrimSpace(text.String()); t != "" {
				*out = append(*out, TodoBullet{NodeID: nodeID, Text: t, Checked: checked})
			}
		}
		for _, ch := range children(item) {
			if ch["type"] == "bulletList" {
				collectUnlinked(ch, out)
			}
		}
	}
}
