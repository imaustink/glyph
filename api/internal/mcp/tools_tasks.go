package mcp

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/glyph/api/internal/model"
	"github.com/glyph/api/internal/pmmd"
	"github.com/google/uuid"
)

const (
	defaultTaskLimit = 50
	maxTaskLimit     = 500
)

func taskTools() []*tool {
	return []*tool{
		{
			name:        "list_tasks",
			title:       "List tasks",
			description: "List tasks, sorted like the board's Smart Sort (open first, then by due date, then priority). Done and cancelled tasks are excluded unless include_completed is true or status asks for them. All filters combine with AND.",
			input: object(map[string]schema{
				"status":            enumArray("Only these statuses.", statusValues...),
				"priority":          enumArray("Only these priorities.", priorityValues...),
				"tags":              strArray("Only tasks with at least one of these tags."),
				"page_id":           str("Only tasks linked to this page."),
				"folder_id":         str("Only tasks in this folder: linked to a page inside it, or assigned to its board."),
				"workspace":         str(`Only tasks in this workspace ("personal" or an org id).`),
				"due_before":        str("Only tasks due on or before this date (YYYY-MM-DD)."),
				"due_after":         str("Only tasks due on or after this date (YYYY-MM-DD)."),
				"query":             str("Only tasks whose title or description contains this text."),
				"include_completed": boolean("Include done and cancelled tasks (default false)."),
				"limit":             integer("Maximum results (default 50).", 1, maxTaskLimit),
			}),
			scopes:     []model.OAuthScope{model.ScopeTaskRead},
			readOnly:   true,
			idempotent: true,
			run:        listTasks,
		},
		{
			name:        "get_task",
			title:       "Read task",
			description: "Read one task by id.",
			input: object(map[string]schema{
				"task_id": str("The task id."),
			}, "task_id"),
			scopes:     []model.OAuthScope{model.ScopeTaskRead},
			readOnly:   true,
			idempotent: true,
			run:        getTask,
		},
		{
			name:        "create_task",
			title:       "Create task",
			description: "Create a task. With page_id, the task is linked to that note and added as a bullet under its TODO heading (created if missing), exactly as if it had been written there — this needs page write access too. Without page_id it's a standalone task, optionally on a folder's board.",
			input: object(map[string]schema{
				"title":       str("Task title."),
				"description": str("Longer description."),
				"status":      enum("Initial status (default todo).", statusValues...),
				"priority":    enum("Priority (default none).", priorityValues...),
				"tags":        strArray("Tags."),
				"due_date":    str("Due date, YYYY-MM-DD."),
				"page_id":     str("Note to link the task to (adds a bullet under its TODO heading)."),
				"folder_id":   str("Folder board to put a standalone task on."),
				"workspace":   str(`"personal" or an org id (default: the page's or folder's workspace, else personal).`),
			}, "title"),
			scopes: []model.OAuthScope{model.ScopeTaskWrite},
			run:    createTask,
		},
		{
			name:        "update_task",
			title:       "Update task",
			description: "Change a task's title, description, status, priority, tags, or due date. Only the fields you pass change; pass due_date as null or \"\" to clear it.",
			input: object(map[string]schema{
				"task_id":     str("The task id."),
				"title":       str("New title."),
				"description": str("New description."),
				"status":      enum("New status.", statusValues...),
				"priority":    enum("New priority.", priorityValues...),
				"tags":        strArray("Replacement tag list."),
				"due_date":    schema{"type": []string{"string", "null"}, "description": "New due date (YYYY-MM-DD), or null/\"\" to clear."},
			}, "task_id"),
			scopes:     []model.OAuthScope{model.ScopeTaskWrite},
			idempotent: true,
			run:        updateTask,
		},
		{
			name:        "delete_task",
			title:       "Delete task",
			description: "Permanently delete a task. Prefer update_task with status \"done\" or \"cancelled\" unless the user asked for deletion. A bullet linked to it in a note stays in the note.",
			input: object(map[string]schema{
				"task_id": str("The task id."),
			}, "task_id"),
			scopes:      []model.OAuthScope{model.ScopeTaskWrite},
			destructive: true,
			idempotent:  true,
			run:         deleteTask,
		},
	}
}

func listTasks(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Status           []string `json:"status"`
		Priority         []string `json:"priority"`
		Tags             []string `json:"tags"`
		PageID           string   `json:"page_id"`
		FolderID         string   `json:"folder_id"`
		Workspace        string   `json:"workspace"`
		DueBefore        string   `json:"due_before"`
		DueAfter         string   `json:"due_after"`
		Query            string   `json:"query"`
		IncludeCompleted bool     `json:"include_completed"`
		Limit            int      `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	for _, s := range args.Status {
		if err := validEnum("status", s, statusValues); err != nil {
			return nil, err
		}
	}
	for _, p := range args.Priority {
		if err := validEnum("priority", p, priorityValues); err != nil {
			return nil, err
		}
	}
	for field, v := range map[string]string{"due_before": args.DueBefore, "due_after": args.DueAfter} {
		if v != "" {
			if err := validDate(field, v); err != nil {
				return nil, err
			}
		}
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultTaskLimit
	}

	query := url.Values{}
	if args.PageID != "" {
		id, err := parseID("page_id", args.PageID)
		if err != nil {
			return nil, err
		}
		query.Set("sourcePageId", id.String())
	}
	var tasks []*model.Task
	if err := cc.api.get("/tasks", query, &tasks); err != nil {
		return nil, err
	}

	var inFolder func(*model.Task) bool
	if args.FolderID != "" {
		folderID, err := parseID("folder_id", args.FolderID)
		if err != nil {
			return nil, err
		}
		if inFolder, err = cc.folderMembership(folderID); err != nil {
			return nil, err
		}
	}

	statusSet := toSet(args.Status)
	prioritySet := toSet(args.Priority)
	q := strings.ToLower(strings.TrimSpace(args.Query))
	var matched []*model.Task
	for _, t := range tasks {
		if len(statusSet) > 0 {
			if !statusSet[string(t.Status)] {
				continue
			}
		} else if !args.IncludeCompleted && isTerminal(t.Status) {
			continue
		}
		if len(prioritySet) > 0 && !prioritySet[string(t.Priority)] {
			continue
		}
		if len(args.Tags) > 0 && !anyTag(t.Tags, args.Tags) {
			continue
		}
		if args.Workspace != "" && workspaceOf(t.OrgID) != args.Workspace {
			continue
		}
		if args.DueBefore != "" && (t.DueDate == nil || *t.DueDate > args.DueBefore) {
			continue
		}
		if args.DueAfter != "" && (t.DueDate == nil || *t.DueDate < args.DueAfter) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(t.Title), q) && !strings.Contains(strings.ToLower(t.Description), q) {
			continue
		}
		if inFolder != nil && !inFolder(t) {
			continue
		}
		matched = append(matched, t)
	}
	autoSortTasks(matched)
	total := len(matched)
	if len(matched) > limit {
		matched = matched[:limit]
	}
	views := make([]taskView, 0, len(matched))
	for _, t := range matched {
		views = append(views, cc.viewTask(t))
	}
	return map[string]interface{}{"total": total, "returned": len(views), "tasks": views}, nil
}

// folderMembership returns a predicate for "task belongs to this folder's
// board": assigned to the folder directly, or linked to a page anywhere
// below it. Resolving pages needs page read access; without it only direct
// assignment can be checked.
func (cc *callContext) folderMembership(folderID uuid.UUID) (func(*model.Task) bool, error) {
	pagesBelow := map[uuid.UUID]bool{}
	if hasScope(cc.scope, model.ScopePageRead) {
		idx, err := cc.loadPages()
		if err != nil {
			return nil, err
		}
		if f := idx.byID[folderID]; f == nil || f.Type != model.NodeTypeFolder {
			return nil, userError("folder_id is not a folder this connection can see")
		}
		for _, p := range idx.descendants(folderID) {
			pagesBelow[p.ID] = true
		}
	}
	return func(t *model.Task) bool {
		if t.FolderID != nil && *t.FolderID == folderID {
			return true
		}
		return t.SourcePageID != nil && pagesBelow[*t.SourcePageID]
	}, nil
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

func anyTag(have, want []string) bool {
	for _, w := range want {
		if containsFold(have, w) {
			return true
		}
	}
	return false
}

func getTask(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	id, err := parseID("task_id", args.TaskID)
	if err != nil {
		return nil, err
	}
	var t model.Task
	if err := cc.api.get("/tasks/"+id.String(), nil, &t); err != nil {
		return nil, err
	}
	return cc.viewTask(&t), nil
}

func createTask(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Status      string   `json:"status"`
		Priority    string   `json:"priority"`
		Tags        []string `json:"tags"`
		DueDate     string   `json:"due_date"`
		PageID      string   `json:"page_id"`
		FolderID    string   `json:"folder_id"`
		Workspace   string   `json:"workspace"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	title, err := requireString("title", args.Title)
	if err != nil {
		return nil, err
	}
	// Bullet text is a single line; a title with newlines would split it.
	title = strings.Join(strings.Fields(title), " ")
	status := model.StatusTodo
	if args.Status != "" {
		if err := validEnum("status", args.Status, statusValues); err != nil {
			return nil, err
		}
		status = model.TaskStatus(args.Status)
	}
	priority := model.PriorityNone
	if args.Priority != "" {
		if err := validEnum("priority", args.Priority, priorityValues); err != nil {
			return nil, err
		}
		priority = model.Priority(args.Priority)
	}
	var due *string
	if args.DueDate != "" {
		if err := validDate("due_date", args.DueDate); err != nil {
			return nil, err
		}
		due = &args.DueDate
	}
	if args.PageID != "" && args.FolderID != "" {
		return nil, userError("pass page_id or folder_id, not both")
	}
	tags := args.Tags
	if tags == nil {
		tags = []string{}
	}
	task := model.Task{
		Title:       title,
		Description: args.Description,
		Status:      status,
		Priority:    priority,
		Tags:        tags,
		DueDate:     due,
	}

	var page *model.Page
	var content *model.PageContent
	if args.PageID != "" {
		if !hasScope(cc.scope, model.ScopePageWrite) {
			return nil, userError("linking a task to a note needs the page:write permission, which this connection doesn't have; omit page_id to create a standalone task")
		}
		pageID, err := parseID("page_id", args.PageID)
		if err != nil {
			return nil, err
		}
		if page, err = cc.getPage(pageID); err != nil {
			return nil, err
		}
		if page.Type != model.NodeTypePage {
			return nil, userError("page_id is a folder; pass it as folder_id instead")
		}
		// Read the content before creating the task, so a page we can't
		// write to fails the call instead of leaving an unlinked task behind.
		if content, err = cc.getContent(pageID); err != nil {
			return nil, err
		}
		nodeID := uuid.NewString()
		task.SourcePageID = &page.ID
		task.SourceNodeID = &nodeID
		if task.OrgID, err = cc.resolveWorkspace(args.Workspace, &page.OrgID); err != nil {
			return nil, err
		}
	} else {
		var inherit **uuid.UUID
		if args.FolderID != "" {
			folderID, err := parseID("folder_id", args.FolderID)
			if err != nil {
				return nil, err
			}
			task.FolderID = &folderID
			if hasScope(cc.scope, model.ScopePageRead) {
				folder, err := cc.getPage(folderID)
				if err != nil {
					return nil, err
				}
				if folder.Type != model.NodeTypeFolder {
					return nil, userError("folder_id is not a folder")
				}
				inherit = &folder.OrgID
			}
		}
		if task.OrgID, err = cc.resolveWorkspace(args.Workspace, inherit); err != nil {
			return nil, err
		}
	}

	var created model.Task
	if err := cc.api.post("/tasks", task, &created); err != nil {
		return nil, err
	}
	out := map[string]interface{}{"task": cc.viewTask(&created)}
	if page == nil {
		return out, nil
	}

	heading := todoHeading(page.TodoTrigger)
	for attempt := 0; ; attempt++ {
		next, err := pmmd.AppendTaskBullet(content.Content, created.Title, *created.SourceNodeID, created.ID.String(), string(created.Status), heading)
		var links *linkResult
		if err == nil {
			// Also link any other unlinked TODO bullets, as the editor would.
			_, links, err = cc.saveWithTodoLinks(page, next, content.SchemaVersion, content.Revision)
		}
		if err == nil {
			out["page_url"] = cc.srv.appURL("/notes/" + page.ID.String())
			links.addTo(out, cc)
			return out, nil
		}
		// The note was edited between our read and write: re-read and retry once.
		var ue userError
		if attempt == 0 && errors.As(err, &ue) {
			if content, err = cc.getContent(page.ID); err == nil {
				continue
			}
		}
		out["warning"] = "task created and linked to the page, but adding its bullet to the note failed: " + err.Error()
		return out, nil
	}
}

// todoHeading returns the heading text AppendTaskBullet should file the
// bullet under: the page's own TODO trigger when it's a plain heading match,
// otherwise the default "TODO".
func todoHeading(tr *model.TodoTriggerConfig) string {
	if tr == nil || tr.MatchMode != model.MatchModeExact || strings.TrimSpace(tr.Pattern) == "" {
		return "TODO"
	}
	if len(tr.BlockTypes) > 0 {
		ok := false
		for _, b := range tr.BlockTypes {
			if b == "heading" || b == "any" {
				ok = true
			}
		}
		if !ok {
			return "TODO"
		}
	}
	return tr.Pattern
}

func updateTask(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args map[string]json.RawMessage
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	var taskID string
	if err := json.Unmarshal(args["task_id"], &taskID); err != nil {
		return nil, userError("task_id is required")
	}
	id, err := parseID("task_id", taskID)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{}
	stringField := func(key, apiKey string, allowed []string, required bool) error {
		v, ok := args[key]
		if !ok {
			return nil
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return userError(key + " must be a string")
		}
		if required && strings.TrimSpace(s) == "" {
			return userError(key + " cannot be empty")
		}
		if allowed != nil {
			if err := validEnum(key, s, allowed); err != nil {
				return err
			}
		}
		body[apiKey] = s
		return nil
	}
	if err := stringField("title", "title", nil, true); err != nil {
		return nil, err
	}
	if err := stringField("description", "description", nil, false); err != nil {
		return nil, err
	}
	if err := stringField("status", "status", statusValues, true); err != nil {
		return nil, err
	}
	if err := stringField("priority", "priority", priorityValues, true); err != nil {
		return nil, err
	}
	if v, ok := args["tags"]; ok {
		var tags []string
		if err := json.Unmarshal(v, &tags); err != nil {
			return nil, userError("tags must be an array of strings")
		}
		if tags == nil {
			tags = []string{}
		}
		body["tags"] = tags
	}
	if v, ok := args["due_date"]; ok {
		var due *string
		if err := json.Unmarshal(v, &due); err != nil {
			return nil, userError("due_date must be a string or null")
		}
		if due == nil || *due == "" {
			body["dueDate"] = nil
		} else {
			if err := validDate("due_date", *due); err != nil {
				return nil, err
			}
			body["dueDate"] = *due
		}
	}
	if len(body) == 0 {
		return nil, userError("nothing to update: pass at least one field besides task_id")
	}
	var updated model.Task
	if err := cc.api.patch("/tasks/"+id.String(), body, &updated); err != nil {
		return nil, err
	}
	return cc.viewTask(&updated), nil
}

func deleteTask(cc *callContext, raw json.RawMessage) (interface{}, error) {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	id, err := parseID("task_id", args.TaskID)
	if err != nil {
		return nil, err
	}
	if err := cc.api.delete("/tasks/" + id.String()); err != nil {
		return nil, err
	}
	return map[string]interface{}{"deleted": id, "at": time.Now().UTC().Format(time.RFC3339)}, nil
}
