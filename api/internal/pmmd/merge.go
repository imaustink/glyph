package pmmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// MergeReplace merges next — a doc parsed from an agent's replacement
// Markdown — against prev, the doc it replaces, so a replace keeps what
// Markdown can't carry (DI-08):
//
//   - Task links: as PreserveTaskLinks.
//   - Unchanged blocks: a block of next whose Markdown equals that of a block
//     in prev is replaced by the prev block itself, keeping its nodeIds,
//     marks Markdown has no syntax for (underline), link target/rel/title,
//     an orderedList's type, and so on.
//   - Edited bullets: an unlinked bullet that isn't unchanged takes the
//     nodeId of the unlinked prev bullet in the same position (right after
//     the prev bullet its predecessor came from), so editing a bullet's text
//     doesn't give it a new identity.
//
// No nodeId appears twice in the result.
func MergeReplace(prev, next json.RawMessage) (json.RawMessage, error) {
	if isEmptyJSON(prev) || isEmptyJSON(next) {
		return next, nil
	}
	next, err := PreserveTaskLinks(prev, next)
	if err != nil {
		return nil, err
	}
	prevAny, err := decodeGeneric(prev)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid prev document: %w", err)
	}
	nextAny, err := decodeGeneric(next)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid next document: %w", err)
	}
	prevDoc, ok1 := prevAny.(map[string]any)
	nextDoc, ok2 := nextAny.(map[string]any)
	if !ok1 || !ok2 {
		return next, nil
	}

	// Index every block of prev by its type and Markdown.
	byKey := map[string][]*prevEntry{}
	var index func(m map[string]any, parent *prevEntry) *prevEntry
	index = func(m map[string]any, parent *prevEntry) *prevEntry {
		e := &prevEntry{m: m, parent: parent}
		for _, c := range genericChildren(m) {
			e.kids = append(e.kids, index(c, e))
		}
		if k, ok := blockKey(m); ok && parent != nil {
			byKey[k] = append(byKey[k], e)
		}
		return e
	}
	index(prevDoc, nil)

	// Swap unchanged blocks of next for their prev originals, outermost
	// first. Taking a prev block also makes its ancestors and descendants
	// unavailable, so no prev node is ever used twice.
	var substitute func(container map[string]any)
	substitute = func(container map[string]any) {
		content, _ := container["content"].([]any)
		for i, ch := range content {
			m, ok := ch.(map[string]any)
			if !ok {
				continue
			}
			if k, ok := blockKey(m); ok {
				if e := firstAvailable(byKey[k]); e != nil {
					e.take()
					content[i] = e.m
					continue
				}
			}
			substitute(m)
		}
	}
	substitute(nextDoc)

	// Carry nodeIds to edited, unlinked bullets by position.
	prevItems := collectListItems(prevDoc)
	prevIndex := map[string]int{}
	for i, it := range prevItems {
		if id := attrString(it, "nodeId"); id != "" {
			if _, dup := prevIndex[id]; !dup {
				prevIndex[id] = i
			}
		}
	}
	present := map[string]bool{}
	nextItems := collectListItems(nextDoc)
	for _, it := range nextItems {
		if id := attrString(it, "nodeId"); id != "" {
			present[id] = true
		}
	}
	last := -1
	for _, it := range nextItems {
		if j, ok := prevIndex[attrString(it, "nodeId")]; ok {
			last = j
			continue
		}
		if attrString(it, "taskId") != "" {
			continue
		}
		cand := last + 1
		if cand >= len(prevItems) || attrString(prevItems[cand], "taskId") != "" {
			continue
		}
		pid := attrString(prevItems[cand], "nodeId")
		if pid == "" || present[pid] {
			continue
		}
		attrs, _ := it["attrs"].(map[string]any)
		if attrs == nil {
			attrs = map[string]any{}
			it["attrs"] = attrs
		}
		attrs["nodeId"] = pid
		present[pid] = true
		last = cand
	}

	dedupeNodeIDs(nextDoc)
	return json.Marshal(nextDoc)
}

type prevEntry struct {
	m      map[string]any
	parent *prevEntry
	kids   []*prevEntry
	used   bool
}

func (e *prevEntry) take() {
	var down func(x *prevEntry)
	down = func(x *prevEntry) {
		x.used = true
		for _, k := range x.kids {
			down(k)
		}
	}
	down(e)
	for p := e.parent; p != nil; p = p.parent {
		p.used = true
	}
}

func firstAvailable(es []*prevEntry) *prevEntry {
	for _, e := range es {
		if !e.used {
			return e
		}
	}
	return nil
}

// blockKey identifies a block by its type and Markdown rendering. Inline
// nodes have no key: they are matched as part of their block.
func blockKey(m map[string]any) (string, bool) {
	t, _ := m["type"].(string)
	switch t {
	case "", "doc", "text", "hardBreak":
		return "", false
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", false
	}
	var n node
	if json.Unmarshal(raw, &n) != nil {
		return "", false
	}
	var md string
	if isEmptyParagraph(&n) {
		md = ""
	} else {
		md = strings.Join(renderBlock(&n, false), "\n")
	}
	return t + "\x00" + md, true
}

func genericChildren(m map[string]any) []map[string]any {
	raw, _ := m["content"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		if cm, ok := c.(map[string]any); ok {
			out = append(out, cm)
		}
	}
	return out
}

func collectListItems(root any) []map[string]any {
	var out []map[string]any
	walkGeneric(root, func(m map[string]any) {
		if m["type"] == "listItem" {
			out = append(out, m)
		}
	})
	return out
}

func attrString(m map[string]any, key string) string {
	attrs, _ := m["attrs"].(map[string]any)
	s, _ := attrs[key].(string)
	return s
}

// dedupeNodeIDs gives every listItem whose nodeId already appeared earlier
// in the doc a fresh one.
func dedupeNodeIDs(root any) {
	seen := map[string]bool{}
	for _, it := range collectListItems(root) {
		id := attrString(it, "nodeId")
		if id == "" {
			continue
		}
		if seen[id] {
			it["attrs"].(map[string]any)["nodeId"] = uuid.NewString()
			continue
		}
		seen[id] = true
	}
}

// SanitizeTaskLinks unlinks bullets whose taskId already appeared earlier in
// the doc (two bullets would edit one task) or that allowed rejects (e.g. a
// task of another page, which would dangle). It returns the doc and the
// dropped taskIds. Repeated nodeIds are re-issued.
func SanitizeTaskLinks(doc json.RawMessage, allowed func(taskID string) bool) (json.RawMessage, []string, error) {
	if isEmptyJSON(doc) {
		return doc, nil, nil
	}
	root, err := decodeGeneric(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("pmmd: invalid document: %w", err)
	}
	dedupeNodeIDs(root)
	var dropped []string
	seen := map[string]bool{}
	for _, it := range collectListItems(root) {
		id := attrString(it, "taskId")
		if id == "" {
			continue
		}
		if seen[id] || (allowed != nil && !allowed(id)) {
			attrs := it["attrs"].(map[string]any)
			delete(attrs, "taskId")
			delete(attrs, "taskStatus")
			dropped = append(dropped, id)
			continue
		}
		seen[id] = true
	}
	out, err := json.Marshal(root)
	return out, dropped, err
}

// ApplyTaskStatuses sets taskStatus (and checked, for done and cancelled) on
// each linked bullet whose task is in statuses, so the doc shows the task
// rows' status rather than the attribute stored when the bullet was last
// edited, which goes stale when a task changes outside the editor.
func ApplyTaskStatuses(doc json.RawMessage, statuses map[string]string) (json.RawMessage, error) {
	if isEmptyJSON(doc) || len(statuses) == 0 {
		return doc, nil
	}
	root, err := decodeGeneric(doc)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid document: %w", err)
	}
	for _, it := range collectListItems(root) {
		s, ok := statuses[attrString(it, "taskId")]
		if !ok {
			continue
		}
		attrs := it["attrs"].(map[string]any)
		attrs["taskStatus"] = s
		attrs["checked"] = s == "done" || s == "cancelled"
	}
	return json.Marshal(root)
}

// ListItemNodeIDs returns the nodeIds of every listItem in doc.
func ListItemNodeIDs(doc json.RawMessage) (map[string]bool, error) {
	out := map[string]bool{}
	if isEmptyJSON(doc) {
		return out, nil
	}
	root, err := decodeGeneric(doc)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid document: %w", err)
	}
	for _, it := range collectListItems(root) {
		if id := attrString(it, "nodeId"); id != "" {
			out[id] = true
		}
	}
	return out, nil
}

// TaskIDs returns the distinct taskIds linked from doc's bullets, in doc
// order.
func TaskIDs(doc json.RawMessage) ([]string, error) {
	if isEmptyJSON(doc) {
		return nil, nil
	}
	root, err := decodeGeneric(doc)
	if err != nil {
		return nil, fmt.Errorf("pmmd: invalid document: %w", err)
	}
	var out []string
	seen := map[string]bool{}
	for _, it := range collectListItems(root) {
		if id := attrString(it, "taskId"); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}
