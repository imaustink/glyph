package mcp

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/pmmd"
	"github.com/google/uuid"
)

// ─── Search ─────────────────────────────────────────────────────────────────

func searchTools() []*tool {
	return []*tool{{
		name:        "search",
		title:       "Search notes and tasks",
		description: "Case-insensitive text search across note titles and content and task titles and descriptions. Title matches come first. Use get_page or get_task to read a result in full.",
		input: object(map[string]schema{
			"query": str("Text to search for."),
			"types": enumArray("Limit to pages and/or tasks (default both).", "page", "task"),
			"limit": integer("Maximum results (default 20).", 1, 100),
		}, "query"),
		// The endpoint drops result types the token can't read, so either
		// read scope is enough to call it.
		readOnly:   true,
		idempotent: true,
		run: func(cc *callContext, raw json.RawMessage) (interface{}, error) {
			var args struct {
				Query string   `json:"query"`
				Types []string `json:"types"`
				Limit int      `json:"limit"`
			}
			if err := decodeArgs(raw, &args); err != nil {
				return nil, err
			}
			q, err := requireString("query", args.Query)
			if err != nil {
				return nil, err
			}
			if !hasScope(cc.scope, model.ScopePageRead) && !hasScope(cc.scope, model.ScopeTaskRead) {
				return nil, userError("this connection can't read pages or tasks")
			}
			params := url.Values{"q": {q}}
			if len(args.Types) > 0 {
				params.Set("types", strings.Join(args.Types, ","))
			}
			if args.Limit > 0 {
				params.Set("limit", strconv.Itoa(args.Limit))
			}
			var results []struct {
				ID       uuid.UUID  `json:"id"`
				Type     string     `json:"type"`
				Title    string     `json:"title"`
				Snippet  string     `json:"snippet,omitempty"`
				Status   string     `json:"status,omitempty"`
				ParentID *uuid.UUID `json:"parentId,omitempty"`
				URL      string     `json:"url"`
			}
			if err := cc.api.get("/search", params, &results); err != nil {
				return nil, err
			}
			for i := range results {
				if results[i].Type == "task" {
					results[i].URL = cc.srv.appURL("/tasks/" + results[i].ID.String())
				} else {
					results[i].URL = cc.srv.appURL("/notes/" + results[i].ID.String())
				}
			}
			return map[string]interface{}{"results": results}, nil
		},
	}}
}

// ─── Lanes ──────────────────────────────────────────────────────────────────

func laneTools() []*tool {
	return []*tool{
		{
			name:        "list_lanes",
			title:       "List board lanes",
			description: "List the columns (lanes) of the user's personal task board, or of a folder's board with folder_id. Each lane is a saved filter over tasks; use get_lane_tasks to see what's in one.",
			input: object(map[string]schema{
				"folder_id": str("A folder whose board to list (default: the personal board)."),
			}),
			scopes:     []model.OAuthScope{model.ScopeLaneRead},
			readOnly:   true,
			idempotent: true,
			run: func(cc *callContext, raw json.RawMessage) (interface{}, error) {
				var args struct {
					FolderID string `json:"folder_id"`
				}
				if err := decodeArgs(raw, &args); err != nil {
					return nil, err
				}
				lanes, err := cc.loadLanes(args.FolderID)
				if err != nil {
					return nil, err
				}
				return map[string]interface{}{"lanes": lanes}, nil
			},
		},
		{
			name:        "get_lane_tasks",
			title:       "List tasks in a lane",
			description: "List the tasks a board lane currently shows, in the lane's sort order. Pass folder_id for a lane on a folder's board.",
			input: object(map[string]schema{
				"lane_id":   str("The lane id (from list_lanes)."),
				"folder_id": str("The folder whose board the lane is on (omit for the personal board)."),
				"limit":     integer("Maximum results (default 50).", 1, maxTaskLimit),
			}, "lane_id"),
			scopes:     []model.OAuthScope{model.ScopeLaneRead, model.ScopeTaskRead},
			readOnly:   true,
			idempotent: true,
			run:        getLaneTasks,
		},
	}
}

func (cc *callContext) loadLanes(folderArg string) ([]*model.Lane, error) {
	var lanes []*model.Lane
	if folderArg == "" {
		if err := cc.api.get("/lanes", nil, &lanes); err != nil {
			return nil, err
		}
	} else {
		folderID, err := parseID("folder_id", folderArg)
		if err != nil {
			return nil, err
		}
		if err := cc.api.get("/folders/"+folderID.String()+"/lanes", nil, &lanes); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(lanes, func(i, j int) bool { return lanes[i].Order < lanes[j].Order })
	return lanes, nil
}

func getLaneTasks(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		LaneID   string `json:"lane_id"`
		FolderID string `json:"folder_id"`
		Limit    int    `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	laneID, err := parseID("lane_id", args.LaneID)
	if err != nil {
		return nil, err
	}
	lanes, err := cc.loadLanes(args.FolderID)
	if err != nil {
		return nil, err
	}
	var lane *model.Lane
	for _, l := range lanes {
		if l.ID == laneID {
			lane = l
		}
	}
	if lane == nil {
		if args.FolderID == "" {
			return nil, userError("lane not found on the personal board; pass folder_id if it's on a folder's board")
		}
		return nil, userError("lane not found on that folder's board")
	}

	var tasks []*model.Task
	if err := cc.api.post("/tasks/filter", lane.FilterSet, &tasks); err != nil {
		return nil, err
	}
	if args.FolderID != "" {
		folderID, _ := uuid.Parse(args.FolderID)
		inFolder, err := cc.folderMembership(folderID)
		if err != nil {
			return nil, err
		}
		kept := tasks[:0:0]
		for _, t := range tasks {
			if inFolder(t) {
				kept = append(kept, t)
			}
		}
		tasks = kept
	}
	sortLaneTasks(tasks, lane.SortConfig)

	limit := args.Limit
	if limit <= 0 {
		limit = defaultTaskLimit
	}
	total := len(tasks)
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	views := make([]taskView, 0, len(tasks))
	for _, t := range tasks {
		views = append(views, cc.viewTask(t))
	}
	return map[string]interface{}{
		"lane":     map[string]interface{}{"id": lane.ID, "title": lane.Title},
		"total":    total,
		"returned": len(views),
		"tasks":    views,
	}, nil
}

// sortLaneTasks applies a lane's sort config: manual order, a single field,
// or the board's Smart Sort.
func sortLaneTasks(tasks []*model.Task, cfg model.SortConfig) {
	switch cfg.Mode {
	case model.SortModeManual:
		pos := make(map[string]int, len(cfg.TaskOrder))
		for i, id := range cfg.TaskOrder {
			pos[id] = i
		}
		autoSortTasks(tasks)
		sort.SliceStable(tasks, func(i, j int) bool {
			pi, iok := pos[tasks[i].ID.String()]
			pj, jok := pos[tasks[j].ID.String()]
			switch {
			case iok && jok:
				return pi < pj
			case iok != jok:
				return iok // ordered tasks first, the rest in Smart Sort order
			}
			return false
		})
	case model.SortModeField:
		if cfg.Field == nil {
			autoSortTasks(tasks)
			return
		}
		desc := cfg.Direction != nil && *cfg.Direction == model.SortDirectionDesc
		key := fieldKey(*cfg.Field)
		sort.SliceStable(tasks, func(i, j int) bool {
			a, b := key(tasks[i]), key(tasks[j])
			if desc {
				return a > b
			}
			return a < b
		})
	default:
		autoSortTasks(tasks)
	}
}

// fieldKey returns a string sort key for a task field. Priorities sort by
// urgency and missing dates sort last ascending.
func fieldKey(field string) func(*model.Task) string {
	switch field {
	case "title":
		return func(t *model.Task) string { return strings.ToLower(t.Title) }
	case "priority":
		return func(t *model.Task) string { return strconv.Itoa(priorityWeight[t.Priority]) }
	case "status":
		return func(t *model.Task) string { return string(t.Status) }
	case "dueDate":
		return func(t *model.Task) string {
			if t.DueDate == nil {
				return "9999-99-99"
			}
			return *t.DueDate
		}
	case "createdAt":
		return func(t *model.Task) string { return t.CreatedAt.UTC().Format(time.RFC3339Nano) }
	default:
		return func(t *model.Task) string { return t.UpdatedAt.UTC().Format(time.RFC3339Nano) }
	}
}

// ─── Templates ──────────────────────────────────────────────────────────────

func templateTools() []*tool {
	return []*tool{
		{
			name:        "list_templates",
			title:       "List note templates",
			description: "List the user's note templates (e.g. a daily-note or meeting template), including each template's content as Markdown.",
			input:       object(map[string]schema{}),
			scopes:      []model.OAuthScope{model.ScopeTemplateRead},
			readOnly:    true,
			idempotent:  true,
			run: func(cc *callContext, _ json.RawMessage) (interface{}, error) {
				var templates []*model.Template
				if err := cc.api.get("/templates", nil, &templates); err != nil {
					return nil, err
				}
				type view struct {
					ID              uuid.UUID  `json:"id"`
					Name            string     `json:"name"`
					TitleTemplate   string     `json:"titleTemplate"`
					IsDefault       bool       `json:"isDefault"`
					DefaultFolderID *uuid.UUID `json:"defaultFolderId,omitempty"`
					Workspace       string     `json:"workspace"`
					Markdown        string     `json:"markdown"`
				}
				out := make([]view, 0, len(templates))
				for _, t := range templates {
					md, _ := pmmd.ToMarkdown(json.RawMessage(t.Content))
					out = append(out, view{
						ID: t.ID, Name: t.Name, TitleTemplate: t.TitleTemplate, IsDefault: t.IsDefault,
						DefaultFolderID: t.DefaultFolderID, Workspace: workspaceOf(t.OrgID), Markdown: md,
					})
				}
				return map[string]interface{}{"templates": out}, nil
			},
		},
		{
			name:        "create_page_from_template",
			title:       "Create page from template",
			description: "Create a note from a template, filling in date tokens like {{date}} the same way the app does. The title defaults to the template's title pattern and the location to the template's default folder.",
			input: object(map[string]schema{
				"template_id": str("The template id (from list_templates)."),
				"title":       str("Title to use instead of the template's title pattern."),
				"parent_id":   str("Folder to create the page in (default: the template's default folder)."),
				"workspace":   str(`"personal" or an org id (default: the parent's workspace, else personal).`),
				"timezone":    str(`IANA time zone for date tokens, e.g. "America/New_York" (default UTC).`),
			}, "template_id"),
			scopes: []model.OAuthScope{model.ScopeTemplateRead, model.ScopePageWrite},
			run:    createPageFromTemplate,
		},
	}
}

func createPageFromTemplate(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		TemplateID string `json:"template_id"`
		Title      string `json:"title"`
		ParentID   string `json:"parent_id"`
		Workspace  string `json:"workspace"`
		Timezone   string `json:"timezone"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	templateID, err := parseID("template_id", args.TemplateID)
	if err != nil {
		return nil, err
	}
	loc := time.UTC
	if args.Timezone != "" {
		if loc, err = time.LoadLocation(args.Timezone); err != nil {
			return nil, userError("unknown timezone: " + args.Timezone)
		}
	}
	now := time.Now().In(loc)

	var tmpl model.Template
	if err := cc.api.get("/templates/"+templateID.String(), nil, &tmpl); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = strings.TrimSpace(evaluateTitleTemplate(tmpl.TitleTemplate, now))
	}
	if title == "" {
		title = tmpl.Name
	}
	parentID, err := parentArg(args.ParentID)
	if err != nil {
		return nil, err
	}
	if parentID == nil {
		parentID = tmpl.DefaultFolderID
	}
	idx, err := cc.loadPages()
	if err != nil {
		return nil, err
	}
	var inherit **uuid.UUID
	if parentID != nil {
		parent := idx.byID[*parentID]
		if parent == nil {
			if args.ParentID != "" {
				return nil, userError("parent_id not found")
			}
			// The template's default folder isn't reachable by this
			// connection; fall back to the top level.
			parentID = nil
		} else {
			inherit = &parent.OrgID
		}
	}
	orgID, err := cc.resolveWorkspace(args.Workspace, inherit)
	if err != nil {
		return nil, err
	}
	doc, err := instantiateTemplateContent(tmpl.Content, now)
	if err != nil {
		return nil, err
	}

	body := model.Page{
		Type:        model.NodeTypePage,
		Title:       title,
		ParentID:    parentID,
		Order:       idx.nextOrder(parentID),
		Tags:        []string{},
		TodoTrigger: tmpl.TodoTrigger,
		OrgID:       orgID,
	}
	var created model.Page
	if err := cc.api.post("/pages", body, &created); err != nil {
		return nil, err
	}
	idx.byID[created.ID] = &created
	out := map[string]interface{}{"page": cc.viewPage(&created, idx)}
	if doc != nil {
		pc, links, err := cc.saveWithTodoLinks(&created, doc, currentSchemaVersion, 0)
		if err != nil {
			out["warning"] = "page created, but writing the template content failed: " + err.Error()
			return out, nil
		}
		out["revision"] = pc.Revision
		links.addTo(out, cc)
	}
	return out, nil
}
