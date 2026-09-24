package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Title/content tokens, ported from src/lib/utils/titleTemplate.ts so a page
// an agent creates from a template reads the same as one made in the app.
var templateToken = regexp.MustCompile(`\{\{([^}]+)\}\}`)

var tokenResolvers = map[string]func(time.Time) string{
	"date":       func(d time.Time) string { return d.Format("2006-01-02") },
	"date-long":  func(d time.Time) string { return d.Format("January 2, 2006") },
	"date-short": func(d time.Time) string { return d.Format("Jan 2, 2006") },
	"day":        func(d time.Time) string { return d.Format("Monday") },
	"week":       func(d time.Time) string { return fmt.Sprintf("W%02d", localWeek(d)) },
	"month":      func(d time.Time) string { return d.Format("January 2006") },
	"year":       func(d time.Time) string { return d.Format("2006") },
	"time":       func(d time.Time) string { return d.Format("15:04") },
}

func evaluateTitleTemplate(expr string, now time.Time) string {
	return templateToken.ReplaceAllStringFunc(expr, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		if r, ok := tokenResolvers[key]; ok {
			return r(now)
		}
		return m
	})
}

// localWeek matches date-fns format(d, "ww") with its default en-US locale:
// weeks start on Sunday and week 1 is the week containing January 1, so the
// last days of December can already be week 1 of the next year.
func localWeek(d time.Time) int {
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	weekStart := d.AddDate(0, 0, -int(d.Weekday()))
	if weekStart.AddDate(0, 0, 6).Year() > d.Year() {
		return 1
	}
	jan1 := time.Date(d.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	firstWeekStart := jan1.AddDate(0, 0, -int(jan1.Weekday()))
	return int(weekStart.Sub(firstWeekStart).Hours()/24)/7 + 1
}

// instantiateTemplateContent turns a template's stored ProseMirror JSON
// (a string) into a new page's document: tokens in text are evaluated, and
// every bullet gets a fresh nodeId with any task link dropped, since a
// template's bullets must not alias the bullets or tasks of pages already
// made from it. Returns nil for an empty template.
func instantiateTemplateContent(content string, now time.Time) (json.RawMessage, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var doc interface{}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil, userError("the template's content is not valid")
	}
	var walk func(n interface{})
	walk = func(n interface{}) {
		m, ok := n.(map[string]interface{})
		if !ok {
			return
		}
		if t, ok := m["text"].(string); ok {
			m["text"] = evaluateTitleTemplate(t, now)
		}
		if m["type"] == "listItem" {
			attrs, _ := m["attrs"].(map[string]interface{})
			if attrs == nil {
				attrs = map[string]interface{}{}
			}
			attrs["nodeId"] = uuid.NewString()
			delete(attrs, "taskId")
			delete(attrs, "taskStatus")
			m["attrs"] = attrs
		}
		if children, ok := m["content"].([]interface{}); ok {
			for _, ch := range children {
				walk(ch)
			}
		}
	}
	walk(doc)
	return json.Marshal(doc)
}
