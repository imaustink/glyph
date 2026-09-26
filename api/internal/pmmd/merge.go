package pmmd

import "encoding/json"

// MergeReplace merges next (a doc parsed from an agent's replacement
// Markdown) against prev (the doc it replaces). Not implemented yet.
func MergeReplace(prev, next json.RawMessage) (json.RawMessage, error) { return next, nil }

// SanitizeTaskLinks drops duplicate and foreign task links. Not implemented yet.
func SanitizeTaskLinks(doc json.RawMessage, allowed func(taskID string) bool) (json.RawMessage, []string, error) {
	return doc, nil, nil
}

// ApplyTaskStatuses shows each linked bullet's task status. Not implemented yet.
func ApplyTaskStatuses(doc json.RawMessage, statuses map[string]string) (json.RawMessage, error) {
	return doc, nil
}

// ListItemNodeIDs returns the nodeIds of every listItem. Not implemented yet.
func ListItemNodeIDs(doc json.RawMessage) (map[string]bool, error) { return map[string]bool{}, nil }

// TaskIDs returns the taskIds linked in doc. Not implemented yet.
func TaskIDs(doc json.RawMessage) ([]string, error) { return nil, nil }
