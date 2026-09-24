package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
)

// tool is one MCP tool: its advertised descriptor plus the scopes it needs
// and its implementation.
type tool struct {
	name        string
	title       string
	description string
	input       schema
	// scopes are all required. A write scope satisfies the matching read
	// scope, mirroring how the API itself treats them.
	scopes      []model.OAuthScope
	readOnly    bool
	destructive bool
	idempotent  bool
	run         func(cc *callContext, args json.RawMessage) (interface{}, error)
}

type toolDescriptor struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema schema          `json:"inputSchema"`
	Annotations toolAnnotations `json:"annotations"`
}

type toolAnnotations struct {
	Title           string `json:"title"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint bool   `json:"destructiveHint"`
	IdempotentHint  bool   `json:"idempotentHint"`
	OpenWorldHint   bool   `json:"openWorldHint"`
}

func (t *tool) descriptor() toolDescriptor {
	return toolDescriptor{
		Name:        t.name,
		Title:       t.title,
		Description: t.description,
		InputSchema: t.input,
		Annotations: toolAnnotations{
			Title:           t.title,
			ReadOnlyHint:    t.readOnly,
			DestructiveHint: t.destructive,
			IdempotentHint:  t.idempotent,
			OpenWorldHint:   false,
		},
	}
}

var writeFor = map[model.OAuthScope]model.OAuthScope{
	model.ScopePageRead:     model.ScopePageWrite,
	model.ScopeTaskRead:     model.ScopeTaskWrite,
	model.ScopeTemplateRead: model.ScopeTemplateWrite,
}

// hasScope reports whether the token satisfies s (a write scope also
// satisfies its read counterpart).
func hasScope(ts *model.TokenScope, s model.OAuthScope) bool {
	if ts == nil {
		return false
	}
	if ts.HasScope(s) {
		return true
	}
	w, ok := writeFor[s]
	return ok && ts.HasScope(w)
}

func (t *tool) missingScopes(ts *model.TokenScope) []string {
	var missing []string
	for _, s := range t.scopes {
		if !hasScope(ts, s) {
			missing = append(missing, string(s))
		}
	}
	return missing
}

// tools is the full catalog, in the order tools/list presents them. It is
// populated in init() so tool implementations can reference toolsByName.
var (
	tools       []*tool
	toolsByName map[string]*tool
)

func init() {
	tools = append(tools, workspaceTools()...)
	tools = append(tools, searchTools()...)
	tools = append(tools, pageTools()...)
	tools = append(tools, taskTools()...)
	tools = append(tools, laneTools()...)
	tools = append(tools, templateTools()...)
	toolsByName = make(map[string]*tool, len(tools))
	for _, t := range tools {
		if _, dup := toolsByName[t.name]; dup {
			panic("mcp: duplicate tool " + t.name)
		}
		toolsByName[t.name] = t
	}
}

// ─── JSON Schema helpers ────────────────────────────────────────────────────

type schema map[string]interface{}

func object(props map[string]schema, required ...string) schema {
	s := schema{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) schema { return schema{"type": "string", "description": desc} }

func enum(desc string, values ...string) schema {
	return schema{"type": "string", "description": desc, "enum": values}
}

func integer(desc string, minimum, maximum int) schema {
	return schema{"type": "integer", "description": desc, "minimum": minimum, "maximum": maximum}
}

func boolean(desc string) schema { return schema{"type": "boolean", "description": desc} }

func strArray(desc string) schema {
	return schema{"type": "array", "description": desc, "items": schema{"type": "string"}}
}

func enumArray(desc string, values ...string) schema {
	return schema{"type": "array", "description": desc, "items": schema{"type": "string", "enum": values}}
}

var (
	statusValues   = []string{"todo", "in-progress", "done", "cancelled"}
	priorityValues = []string{"urgent", "high", "medium", "low", "none"}
)

// ─── Argument helpers ───────────────────────────────────────────────────────

func decodeArgs(raw json.RawMessage, v interface{}) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return userError("invalid arguments: " + err.Error())
	}
	return nil
}

func parseID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, userError(fmt.Sprintf("%s must be a UUID (got %q)", field, raw))
	}
	return id, nil
}

func requireString(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", userError(field + " is required")
	}
	return v, nil
}

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func validDate(field, v string) error {
	if !isoDate.MatchString(v) {
		return userError(field + " must be a date in YYYY-MM-DD format")
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return userError(field + " is not a valid date")
	}
	return nil
}

func validEnum(field, v string, allowed []string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return userError(fmt.Sprintf("%s must be one of %s", field, strings.Join(allowed, ", ")))
}

// ─── Workspaces ─────────────────────────────────────────────────────────────

const personalWorkspace = "personal"

// resolveWorkspace maps a workspace argument ("personal", an org id, or
// empty) to the orgId to create a resource in. inherit, when non-nil, is the
// workspace of a parent the new resource is created under and wins over the
// default when no workspace was given.
func (cc *callContext) resolveWorkspace(arg string, inherit **uuid.UUID) (*uuid.UUID, error) {
	arg = strings.TrimSpace(arg)
	switch {
	case arg == "" && inherit != nil:
		return *inherit, nil
	case arg == "":
		if cc.scope.Personal {
			return nil, nil
		}
		if len(cc.scope.OrgIDs) == 1 {
			id := cc.scope.OrgIDs[0]
			return &id, nil
		}
		return nil, userError("this connection can reach several workspaces; pass workspace (see list_workspaces)")
	case arg == personalWorkspace:
		if !cc.scope.Personal {
			return nil, userError("this connection wasn't granted the personal workspace")
		}
		return nil, nil
	default:
		id, err := parseID("workspace", arg)
		if err != nil {
			return nil, userError(`workspace must be "personal" or an org id from list_workspaces`)
		}
		if !cc.scope.HasOrg(id) {
			return nil, userError("this connection wasn't granted that workspace")
		}
		return &id, nil
	}
}

func workspaceTools() []*tool {
	return []*tool{{
		name:        "list_workspaces",
		title:       "List workspaces",
		description: `List the workspaces this connection can reach: "personal" (the user's own notes and tasks) and/or organizations. Use a workspace id as the workspace argument when creating pages or tasks.`,
		input:       object(map[string]schema{}),
		readOnly:    true,
		idempotent:  true,
		run: func(cc *callContext, _ json.RawMessage) (interface{}, error) {
			type ws struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Kind string `json:"kind"`
			}
			out := []ws{}
			if cc.scope.Personal {
				out = append(out, ws{ID: personalWorkspace, Name: "Personal workspace", Kind: "personal"})
			}
			for _, id := range cc.scope.OrgIDs {
				name := id.String()
				if org, err := cc.srv.Orgs.GetByID(cc.api.ctx, id); err == nil {
					name = org.Name
				}
				out = append(out, ws{ID: id.String(), Name: name, Kind: "org"})
			}
			return out, nil
		},
	}}
}

// ─── Shared views ───────────────────────────────────────────────────────────

// workspaceOf renders a resource's orgId the way tools accept it back.
func workspaceOf(orgID *uuid.UUID) string {
	if orgID == nil {
		return personalWorkspace
	}
	return orgID.String()
}

type taskView struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description,omitempty"`
	Status       string     `json:"status"`
	Priority     string     `json:"priority"`
	Tags         []string   `json:"tags"`
	DueDate      *string    `json:"dueDate"`
	SourcePageID *uuid.UUID `json:"sourcePageId,omitempty"`
	FolderID     *uuid.UUID `json:"folderId,omitempty"`
	Workspace    string     `json:"workspace"`
	Link         string     `json:"link,omitempty"`
	URL          string     `json:"url"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func (cc *callContext) viewTask(t *model.Task) taskView {
	v := taskView{
		ID:           t.ID,
		Title:        t.Title,
		Description:  t.Description,
		Status:       string(t.Status),
		Priority:     string(t.Priority),
		Tags:         t.Tags,
		DueDate:      t.DueDate,
		SourcePageID: t.SourcePageID,
		FolderID:     t.FolderID,
		Workspace:    workspaceOf(t.OrgID),
		URL:          cc.srv.appURL("/tasks/" + t.ID.String()),
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if t.Link != nil {
		v.Link = t.Link.URL
	}
	return v
}

var priorityWeight = map[model.Priority]int{
	model.PriorityUrgent: 0, model.PriorityHigh: 1, model.PriorityMedium: 2, model.PriorityLow: 3, model.PriorityNone: 4, "": 4,
}

func isTerminal(s model.TaskStatus) bool {
	return s == model.StatusDone || s == model.StatusCancelled
}

// autoSortTasks mirrors the board's "Smart Sort" (src/lib/sort/
// AutoSortProvider.ts): open before done/cancelled, then due date (undated
// last), then priority, then oldest first. The board's note-priority tie
// breaker is omitted.
func autoSortTasks(tasks []*model.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if ta, tb := isTerminal(a.Status), isTerminal(b.Status); ta != tb {
			return !ta
		}
		switch {
		case a.DueDate != nil && b.DueDate != nil:
			if *a.DueDate != *b.DueDate {
				return *a.DueDate < *b.DueDate
			}
		case a.DueDate != nil:
			return true
		case b.DueDate != nil:
			return false
		}
		if pa, pb := priorityWeight[a.Priority], priorityWeight[b.Priority]; pa != pb {
			return pa < pb
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
}
