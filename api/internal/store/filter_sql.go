package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
)

// allowedTaskFields maps TypeScript field names to their SQL column names.
// Only fields in this map can be used in filter rules — prevents SQL injection.
var allowedTaskFields = map[string]string{
	"status":       "status",
	"priority":     "priority",
	"title":        "title",
	"dueDate":      "due_date",
	"sourcePageId": "source_page_id",
	// "tags" is handled separately (text array column)
}

// BuildTaskFilterSQL converts a FilterSet into a SQL WHERE clause fragment
// and an args slice. The args are appended starting at argOffset (e.g. pass 2
// if $1 is already used for user_id).
//
// Returns ("", nil) when there are no effective rules (caller appends nothing).
func BuildTaskFilterSQL(fs model.FilterSet, argOffset int) (clause string, args []interface{}) {
	if len(fs.Rules) == 0 {
		return "", nil
	}

	conjunction := "AND"
	if fs.Conjunction == model.ConjunctionOr {
		conjunction = "OR"
	}

	var clauses []string
	nextArg := argOffset

	for _, rule := range fs.Rules {
		if rule.Operator == model.FilterOpAny {
			// 'any' means match everything — skip this rule (generates no SQL)
			continue
		}

		if rule.Field == "tags" {
			c, a, n := buildTagsClause(rule, nextArg)
			if c != "" {
				clauses = append(clauses, c)
				args = append(args, a...)
				nextArg = n
			}
			continue
		}

		if rule.Field == "sourcePageTags" {
			c, a, n := buildSourcePageTagsClause(rule, nextArg)
			if c != "" {
				clauses = append(clauses, c)
				args = append(args, a...)
				nextArg = n
			}
			continue
		}

		col, ok := allowedTaskFields[rule.Field]
		if !ok {
			// Skip unknown fields — forward-compat with new TS fields
			continue
		}

		c, a, n := buildScalarClause(col, rule, nextArg)
		if c != "" {
			clauses = append(clauses, c)
			args = append(args, a...)
			nextArg = n
		}
	}

	if len(clauses) == 0 {
		return "", nil
	}

	clause = "(" + strings.Join(clauses, " "+conjunction+" ") + ")"
	return clause, args
}

func buildScalarClause(col string, rule model.FilterRule, offset int) (string, []interface{}, int) {
	switch rule.Operator {
	case model.FilterOpEq:
		return fmt.Sprintf("%s = $%d", col, offset), []interface{}{rule.Value}, offset + 1
	case model.FilterOpNeq:
		return fmt.Sprintf("(%s != $%d OR %s IS NULL)", col, offset, col), []interface{}{rule.Value}, offset + 1
	case model.FilterOpIn:
		vals, err := toStringSlice(rule.Value)
		if err != nil || len(vals) == 0 {
			return "", nil, offset
		}
		placeholders, iargs := buildInArgs(vals, offset)
		return fmt.Sprintf("%s IN (%s)", col, placeholders), iargs, offset + len(vals)
	case model.FilterOpNotIn:
		vals, err := toStringSlice(rule.Value)
		if err != nil || len(vals) == 0 {
			return "", nil, offset
		}
		placeholders, iargs := buildInArgs(vals, offset)
		return fmt.Sprintf("(%s NOT IN (%s) OR %s IS NULL)", col, placeholders, col), iargs, offset + len(vals)
	case model.FilterOpContains:
		return fmt.Sprintf("%s ILIKE $%d", col, offset), []interface{}{"%" + fmt.Sprint(rule.Value) + "%"}, offset + 1
	case model.FilterOpBefore:
		t, err := parseDate(rule.Value)
		if err != nil {
			return "", nil, offset
		}
		return fmt.Sprintf("%s < $%d", col, offset), []interface{}{t}, offset + 1
	case model.FilterOpAfter:
		t, err := parseDate(rule.Value)
		if err != nil {
			return "", nil, offset
		}
		return fmt.Sprintf("%s > $%d", col, offset), []interface{}{t}, offset + 1
	case model.FilterOpExists:
		return fmt.Sprintf("%s IS NOT NULL", col), nil, offset
	case model.FilterOpNotExists:
		return fmt.Sprintf("%s IS NULL", col), nil, offset
	default:
		return "", nil, offset
	}
}

// buildSourcePageTagsClause filters tasks by the tags of their source note
// (source_page_id → pages.tags) using a correlated EXISTS subquery.
func buildSourcePageTagsClause(rule model.FilterRule, offset int) (string, []interface{}, int) {
	const existsBase = "SELECT 1 FROM pages p WHERE p.id = tasks.source_page_id"

	switch rule.Operator {
	case model.FilterOpContains, model.FilterOpEq:
		return fmt.Sprintf("EXISTS (%s AND $%d = ANY(p.tags))", existsBase, offset),
			[]interface{}{fmt.Sprint(rule.Value)}, offset + 1
	case model.FilterOpNeq:
		return fmt.Sprintf("NOT EXISTS (%s AND $%d = ANY(p.tags))", existsBase, offset),
			[]interface{}{fmt.Sprint(rule.Value)}, offset + 1
	case model.FilterOpIn:
		vals, err := toStringSlice(rule.Value)
		if err != nil || len(vals) == 0 {
			return "", nil, offset
		}
		ps, iargs := buildInArgs(vals, offset)
		return fmt.Sprintf("EXISTS (%s AND p.tags && ARRAY[%s])", existsBase, ps), iargs, offset + len(vals)
	case model.FilterOpNotIn:
		vals, err := toStringSlice(rule.Value)
		if err != nil || len(vals) == 0 {
			return "", nil, offset
		}
		ps, iargs := buildInArgs(vals, offset)
		return fmt.Sprintf("NOT EXISTS (%s AND p.tags && ARRAY[%s])", existsBase, ps), iargs, offset + len(vals)
	case model.FilterOpExists:
		return fmt.Sprintf("EXISTS (%s AND array_length(p.tags, 1) > 0)", existsBase), nil, offset
	case model.FilterOpNotExists:
		return fmt.Sprintf("NOT EXISTS (%s AND array_length(p.tags, 1) > 0)", existsBase), nil, offset
	default:
		return "", nil, offset
	}
}

func buildTagsClause(rule model.FilterRule, offset int) (string, []interface{}, int) {
	switch rule.Operator {
	case model.FilterOpContains:
		s := fmt.Sprint(rule.Value)
		return fmt.Sprintf("$%d = ANY(tags)", offset), []interface{}{s}, offset + 1
	case model.FilterOpIn:
		vals, err := toStringSlice(rule.Value)
		if err != nil || len(vals) == 0 {
			return "", nil, offset
		}
		ps := make([]string, len(vals))
		ifaces := make([]interface{}, len(vals))
		for i, v := range vals {
			ps[i] = fmt.Sprintf("$%d", offset+i)
			ifaces[i] = v
		}
		return fmt.Sprintf("tags && ARRAY[%s]", strings.Join(ps, ",")), ifaces, offset + len(vals)
	case model.FilterOpExists:
		return "array_length(tags, 1) > 0", nil, offset
	case model.FilterOpNotExists:
		return "(tags IS NULL OR array_length(tags, 1) = 0)", nil, offset
	default:
		return "", nil, offset
	}
}

func buildInArgs(vals []string, offset int) (string, []interface{}) {
	placeholders := make([]string, len(vals))
	args := make([]interface{}, len(vals))
	for i, v := range vals {
		placeholders[i] = fmt.Sprintf("$%d", offset+i)
		args[i] = v
	}
	return strings.Join(placeholders, ", "), args
}

func toStringSlice(v interface{}) ([]string, error) {
	switch t := v.(type) {
	case []interface{}:
		result := make([]string, len(t))
		for i, item := range t {
			result[i] = fmt.Sprint(item)
		}
		return result, nil
	case []string:
		return t, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to []string", v)
	}
}

func parseDate(v interface{}) (time.Time, error) {
	s := fmt.Sprint(v)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
	}
	return t, err
}
