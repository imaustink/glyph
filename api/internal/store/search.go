package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PageTextMatch is one page whose content matched a SearchContent query,
// with the page's plain text for building a result snippet.
type PageTextMatch struct {
	PageID uuid.UUID
	Text   string
}

// DocPlainText flattens a ProseMirror JSON document to its text, one block
// per line. Malformed input yields "".
func DocPlainText(doc json.RawMessage) string {
	var root interface{}
	if err := json.Unmarshal(doc, &root); err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n interface{})
	walk = func(n interface{}) {
		m, ok := n.(map[string]interface{})
		if !ok {
			return
		}
		if t, ok := m["text"].(string); ok {
			b.WriteString(t)
		}
		children, _ := m["content"].([]interface{})
		for _, ch := range children {
			walk(ch)
		}
		switch m["type"] {
		case "paragraph", "heading", "codeBlock", "hardBreak":
			b.WriteByte('\n')
		}
	}
	walk(root)
	return strings.TrimSpace(b.String())
}

// escapeLike escapes ILIKE wildcards so a user query matches literally.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// SearchContent returns the pages among pageIDs whose content text contains
// query (case-insensitive), up to limit. Callers pass only page IDs they have
// already resolved as readable (and within any OAuth token scope); the access
// filter is re-applied here anyway so the method is safe on its own.
func (s *pgPageStore) SearchContent(ctx context.Context, userID uuid.UUID, pageIDs []uuid.UUID, query string, limit int) ([]PageTextMatch, error) {
	if len(pageIDs) == 0 || strings.TrimSpace(query) == "" {
		return []PageTextMatch{}, nil
	}
	// A phrase can span several text nodes (any change of formatting splits
	// one), so SQL can't match it directly. Instead SQL narrows to documents
	// whose text values contain every word of the query — jsonb_path_query_array
	// collects just the "text" values, so node type names and attribute keys
	// in the JSON never match — and the phrase itself is checked in Go on the
	// flattened text.
	words := make([]string, 0)
	for _, w := range strings.Fields(query) {
		words = append(words, escapeLike(w))
	}
	const q = `
		SELECT pc.page_id, pc.content
		FROM page_contents pc
		JOIN pages p ON p.id = pc.page_id
		WHERE pc.page_id = ANY($2)
		  AND NOT EXISTS (
			SELECT 1 FROM unnest($3::text[]) AS w
			WHERE jsonb_path_query_array(pc.content, 'strict $.**.text')::text NOT ILIKE '%' || w || '%'
		  )
		  AND (
			p.user_id = $1
			OR (p.org_id IS NOT NULL AND p.is_private = false
			    AND p.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1))
			OR EXISTS (SELECT 1 FROM shares
			           WHERE resource_type = 'page' AND resource_id = p.id AND shared_with_id = $1)
		  )
		ORDER BY p.updated_at DESC`
	rows, err := s.pool.Query(ctx, q, userID, pageIDs, words)
	if err != nil {
		return nil, fmt.Errorf("search content: %w", err)
	}
	defer rows.Close()
	out := make([]PageTextMatch, 0)
	for rows.Next() {
		var id uuid.UUID
		var content json.RawMessage
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("search content scan: %w", err)
		}
		text := DocPlainText(content)
		if !ContainsPhrase(text, query) {
			continue
		}
		out = append(out, PageTextMatch{PageID: id, Text: text})
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// ContainsPhrase reports whether text contains query case-insensitively,
// treating any run of whitespace (including line breaks between blocks) as a
// single space on both sides.
func ContainsPhrase(text, query string) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	q := norm(query)
	return q != "" && strings.Contains(norm(text), q)
}
