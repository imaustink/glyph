package store

import (
	"strings"
	"testing"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── BuildTaskFilterSQL ───────────────────────────────────────────────────────

func TestBuildTaskFilterSQL_EmptyRules(t *testing.T) {
	clause, args := BuildTaskFilterSQL(model.FilterSet{Rules: []model.FilterRule{}}, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
}

func TestBuildTaskFilterSQL_NilRules(t *testing.T) {
	clause, args := BuildTaskFilterSQL(model.FilterSet{}, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
}

func TestBuildTaskFilterSQL_AnyOperatorSkipped(t *testing.T) {
	fs := model.FilterSet{
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpAny, Value: nil},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
}

func TestBuildTaskFilterSQL_UnknownFieldSkipped(t *testing.T) {
	fs := model.FilterSet{
		Rules: []model.FilterRule{
			{Field: "unknownField", Operator: model.FilterOpEq, Value: "val"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
}

func TestBuildTaskFilterSQL_SingleScalarEq(t *testing.T) {
	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpEq, Value: "done"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.Equal(t, "(status = $2)", clause)
	assert.Equal(t, []interface{}{"done"}, args)
}

func TestBuildTaskFilterSQL_MultipleRulesAnd(t *testing.T) {
	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpEq, Value: "done"},
			{Field: "priority", Operator: model.FilterOpEq, Value: "high"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.True(t, strings.HasPrefix(clause, "("))
	assert.True(t, strings.HasSuffix(clause, ")"))
	assert.Contains(t, clause, " AND ")
	assert.Equal(t, []interface{}{"done", "high"}, args)
}

func TestBuildTaskFilterSQL_MultipleRulesOr(t *testing.T) {
	fs := model.FilterSet{
		Conjunction: model.ConjunctionOr,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpEq, Value: "done"},
			{Field: "priority", Operator: model.FilterOpEq, Value: "high"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.Contains(t, clause, " OR ")
	assert.Equal(t, []interface{}{"done", "high"}, args)
}

func TestBuildTaskFilterSQL_TagsField(t *testing.T) {
	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "tags", Operator: model.FilterOpContains, Value: "important"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.Contains(t, clause, "ANY(tags)")
	assert.Equal(t, []interface{}{"important"}, args)
}

func TestBuildTaskFilterSQL_ArgOffsetAdvances(t *testing.T) {
	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpEq, Value: "todo"},
			{Field: "priority", Operator: model.FilterOpEq, Value: "high"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 3) // start at $3
	assert.Contains(t, clause, "$3")
	assert.Contains(t, clause, "$4")
	assert.Equal(t, []interface{}{"todo", "high"}, args)
}

func TestBuildTaskFilterSQL_AnyMixedWithReal(t *testing.T) {
	// 'any' rules are skipped; only real rules produce clauses.
	fs := model.FilterSet{
		Conjunction: model.ConjunctionAnd,
		Rules: []model.FilterRule{
			{Field: "status", Operator: model.FilterOpAny},
			{Field: "priority", Operator: model.FilterOpEq, Value: "high"},
		},
	}
	clause, args := BuildTaskFilterSQL(fs, 2)
	assert.Equal(t, "(priority = $2)", clause)
	assert.Equal(t, []interface{}{"high"}, args)
}

// ─── buildScalarClause ────────────────────────────────────────────────────────

func TestBuildScalarClause_Eq(t *testing.T) {
	rule := model.FilterRule{Field: "status", Operator: model.FilterOpEq, Value: "done"}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "status = $2", clause)
	assert.Equal(t, []interface{}{"done"}, args)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_Neq(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpNeq, Value: "todo"}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "(status != $2 OR status IS NULL)", clause)
	assert.Equal(t, []interface{}{"todo"}, args)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_In_WithValues(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpIn, Value: []interface{}{"done", "cancelled"}}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "status IN ($2, $3)", clause)
	assert.Equal(t, []interface{}{"done", "cancelled"}, args)
	assert.Equal(t, 4, next)
}

func TestBuildScalarClause_In_EmptySlice(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpIn, Value: []interface{}{}}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_In_StringSlice(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpIn, Value: []string{"high", "urgent"}}
	clause, args, next := buildScalarClause("priority", rule, 1)
	assert.Equal(t, "priority IN ($1, $2)", clause)
	assert.Len(t, args, 2)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_In_InvalidType(t *testing.T) {
	// When value is not a slice, toStringSlice returns error → skip
	rule := model.FilterRule{Operator: model.FilterOpIn, Value: 42}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_NotIn_WithValues(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpNotIn, Value: []interface{}{"done"}}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "(status NOT IN ($2) OR status IS NULL)", clause)
	assert.Equal(t, []interface{}{"done"}, args)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_NotIn_EmptySlice(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpNotIn, Value: []interface{}{}}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_NotIn_InvalidType(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpNotIn, Value: 42}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_Contains(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpContains, Value: "meeting"}
	clause, args, next := buildScalarClause("title", rule, 2)
	assert.Equal(t, "title ILIKE $2", clause)
	assert.Equal(t, []interface{}{"%meeting%"}, args)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_Before_RFC3339(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpBefore, Value: "2024-01-15T10:30:00Z"}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "due_date < $2", clause)
	require.Len(t, args, 1)
	_, ok := args[0].(time.Time)
	assert.True(t, ok)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_Before_DateOnly(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpBefore, Value: "2024-06-01"}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "due_date < $2", clause)
	require.Len(t, args, 1)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_Before_InvalidDate(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpBefore, Value: "not-a-date"}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_After_Valid(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpAfter, Value: "2024-01-01"}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "due_date > $2", clause)
	require.Len(t, args, 1)
	assert.Equal(t, 3, next)
}

func TestBuildScalarClause_After_Invalid(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpAfter, Value: "garbage"}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_Exists(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpExists}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "due_date IS NOT NULL", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_NotExists(t *testing.T) {
	rule := model.FilterRule{Operator: model.FilterOpNotExists}
	clause, args, next := buildScalarClause("due_date", rule, 2)
	assert.Equal(t, "due_date IS NULL", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildScalarClause_Default_Unknown(t *testing.T) {
	rule := model.FilterRule{Operator: "unknown_op"}
	clause, args, next := buildScalarClause("status", rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

// ─── buildTagsClause ──────────────────────────────────────────────────────────

func TestBuildTagsClause_Contains(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpContains, Value: "urgent"}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Equal(t, "$2 = ANY(tags)", clause)
	assert.Equal(t, []interface{}{"urgent"}, args)
	assert.Equal(t, 3, next)
}

func TestBuildTagsClause_In_WithValues(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpIn, Value: []interface{}{"work", "personal"}}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Contains(t, clause, "tags && ARRAY[")
	assert.Contains(t, clause, "$2")
	assert.Contains(t, clause, "$3")
	assert.Len(t, args, 2)
	assert.Equal(t, 4, next)
}

func TestBuildTagsClause_In_StringSlice(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpIn, Value: []string{"a", "b"}}
	clause, args, next := buildTagsClause(rule, 1)
	assert.Contains(t, clause, "tags && ARRAY[")
	assert.Equal(t, 3, next)
	_ = args
}

func TestBuildTagsClause_In_EmptySlice(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpIn, Value: []interface{}{}}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildTagsClause_In_InvalidType(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpIn, Value: 42}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildTagsClause_Exists(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpExists}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Equal(t, "array_length(tags, 1) > 0", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildTagsClause_NotExists(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: model.FilterOpNotExists}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Equal(t, "(tags IS NULL OR array_length(tags, 1) = 0)", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

func TestBuildTagsClause_Default(t *testing.T) {
	rule := model.FilterRule{Field: "tags", Operator: "unknown"}
	clause, args, next := buildTagsClause(rule, 2)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
	assert.Equal(t, 2, next)
}

// ─── toStringSlice ────────────────────────────────────────────────────────────

func TestToStringSlice_InterfaceSlice(t *testing.T) {
	input := []interface{}{1, 2.5, "three"}
	result, err := toStringSlice(input)
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2.5", "three"}, result)
}

func TestToStringSlice_StringSlice(t *testing.T) {
	input := []string{"a", "b", "c"}
	result, err := toStringSlice(input)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestToStringSlice_Default_Error(t *testing.T) {
	result, err := toStringSlice(42)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestToStringSlice_Default_MapError(t *testing.T) {
	result, err := toStringSlice(map[string]int{"a": 1})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// ─── parseDate ────────────────────────────────────────────────────────────────

func TestParseDate_RFC3339(t *testing.T) {
	t1, err := parseDate("2024-01-15T10:30:00Z")
	require.NoError(t, err)
	assert.Equal(t, 2024, t1.Year())
	assert.Equal(t, time.January, t1.Month())
	assert.Equal(t, 15, t1.Day())
}

func TestParseDate_DateOnly(t *testing.T) {
	t1, err := parseDate("2024-06-21")
	require.NoError(t, err)
	assert.Equal(t, 2024, t1.Year())
	assert.Equal(t, time.June, t1.Month())
	assert.Equal(t, 21, t1.Day())
}

func TestParseDate_Invalid(t *testing.T) {
	_, err := parseDate("not-a-date")
	assert.Error(t, err)
}

func TestParseDate_Empty(t *testing.T) {
	_, err := parseDate("")
	assert.Error(t, err)
}

func TestParseDate_NonStringType(t *testing.T) {
	// parseDate calls fmt.Sprint(v) on the value, so int becomes "123" which is invalid date
	_, err := parseDate(12345)
	assert.Error(t, err)
}
