package seed

import "github.com/glyph/api/internal/model"

// ─── ProseMirror JSON builders ───────────────────────────────────────────────
//
// Documents use the editor schema (src/lib/editor/schema.ts): StarterKit
// nodes and marks, tables, and listItem with the TaskLink attributes. The
// content validator rejects or strips anything else.

type pmNode = map[string]any

func doc(blocks ...pmNode) pmNode { return pmNode{"type": "doc", "content": blocks} }

func heading(level int, inline ...pmNode) pmNode {
	return pmNode{"type": "heading", "attrs": map[string]any{"level": level}, "content": inline}
}

func paragraph(inline ...pmNode) pmNode {
	if len(inline) == 0 {
		return pmNode{"type": "paragraph"}
	}
	return pmNode{"type": "paragraph", "content": inline}
}

func text(s string) pmNode { return pmNode{"type": "text", "text": s} }

func marked(s string, marks ...string) pmNode {
	ms := make([]pmNode, len(marks))
	for i, m := range marks {
		ms[i] = pmNode{"type": m}
	}
	return pmNode{"type": "text", "text": s, "marks": ms}
}

func bold(s string) pmNode   { return marked(s, "bold") }
func italic(s string) pmNode { return marked(s, "italic") }
func code(s string) pmNode   { return marked(s, "code") }

func link(s, href string) pmNode {
	return pmNode{"type": "text", "text": s, "marks": []pmNode{{
		"type":  "link",
		"attrs": map[string]any{"href": href, "target": "_blank", "rel": "noopener noreferrer nofollow"},
	}}}
}

// item is a plain (non-task) bullet holding one paragraph.
func item(inline ...pmNode) pmNode {
	return pmNode{"type": "listItem", "content": []pmNode{paragraph(inline...)}}
}

func bullets(items ...pmNode) pmNode { return pmNode{"type": "bulletList", "content": items} }

func numbered(items ...pmNode) pmNode {
	return pmNode{"type": "orderedList", "attrs": map[string]any{"start": 1}, "content": items}
}

func quote(blocks ...pmNode) pmNode { return pmNode{"type": "blockquote", "content": blocks} }

func codeBlock(language, src string) pmNode {
	return pmNode{"type": "codeBlock", "attrs": map[string]any{"language": language}, "content": []pmNode{text(src)}}
}

func rule() pmNode { return pmNode{"type": "horizontalRule"} }

// table builds a table whose first row is a header row.
func table(rows ...[]string) pmNode {
	out := make([]pmNode, len(rows))
	for r, cells := range rows {
		cellType := "tableCell"
		if r == 0 {
			cellType = "tableHeader"
		}
		cs := make([]pmNode, len(cells))
		for i, c := range cells {
			cs[i] = pmNode{"type": cellType, "content": []pmNode{paragraph(text(c))}}
		}
		out[r] = pmNode{"type": "tableRow", "content": cs}
	}
	return pmNode{"type": "table", "content": out}
}

// ─── Dataset ─────────────────────────────────────────────────────────────────

// todoFn renders the i-th task of a note as its linked TODO bullet.
type todoFn func(i int) pmNode

type taskSpec struct {
	title       string
	description string
	status      model.TaskStatus
	priority    model.Priority
	tags        []string
	due         *int // days from the seed date; nil = no due date
	link        *model.LinkMeta
}

type laneSpec struct {
	title  string
	order  int
	filter model.FilterSet
	sort   model.SortConfig
}

type treeNode struct {
	folder   bool
	title    string
	order    int
	tags     []string
	priority model.Priority
	// Notes: their bullet-linked tasks, and the document (which places them
	// under a TODO heading via todo(i)).
	tasks   []taskSpec
	content func(todo todoFn) pmNode
	// Folders: standalone board tasks, board lanes and children.
	boardTasks []taskSpec
	lanes      []laneSpec
	children   []treeNode
}

type templateSpec struct {
	name          string
	titleTemplate string
	isDefault     bool
	content       pmNode
}

func days(n int) *int { return &n }

func strp(s string) *string { return &s }

func rules(conj model.FilterConjunction, rs ...model.FilterRule) model.FilterSet {
	return model.FilterSet{Conjunction: conj, Rules: rs}
}

func fr(id, field string, op model.FilterOperator, value any) model.FilterRule {
	return model.FilterRule{ID: id, Field: field, Operator: op, Value: value}
}

func autoSort() model.SortConfig { return model.SortConfig{Mode: model.SortModeAuto} }

func fieldSort(field string, dir model.SortDirection) model.SortConfig {
	return model.SortConfig{Mode: model.SortModeField, Field: &field, Direction: &dir}
}

// demoTree is the page tree of the demo workspace. The welcome note has the
// lowest order so it sorts first: a user who lands on "/" with no notes of
// their own is taken to it.
func demoTree() []treeNode {
	return []treeNode{
		welcomeNote(),
		productLaunchFolder(),
		engineeringFolder(),
	}
}

func welcomeNote() treeNode {
	return treeNode{
		title: "Welcome to Glyph",
		order: 0,
		tags:  []string{"getting-started"},
		tasks: []taskSpec{
			{title: "Read the welcome note", status: model.StatusDone, priority: model.PriorityNone, tags: []string{"onboarding"}},
			{title: "Check off this task to mark it done", status: model.StatusTodo, priority: model.PriorityNone, tags: []string{"onboarding"}},
			{title: "Open the Task Board and move a card to another lane", status: model.StatusTodo, priority: model.PriorityLow,
				tags: []string{"onboarding"}, due: days(1)},
			{title: "Create a note from the Meeting Notes template", status: model.StatusTodo, priority: model.PriorityMedium,
				tags: []string{"onboarding"}, due: days(3),
				description: "Use the arrow next to the New page button in the sidebar to pick a template."},
		},
		content: func(todo todoFn) pmNode {
			return doc(
				paragraph(
					text("This is the "), bold("demo workspace"), text(" of a Glyph preview environment. "),
					text("Everything here belongs to the "), italic("Glyph Demo"),
					text(" organization, and everyone who signs in to this preview is a member, so you can read and edit all of it. Go ahead and change things."),
				),
				heading(2, text("Notes and tasks")),
				paragraph(text(trimLines(`Bullets under a heading named TODO become tasks. A bullet and its task stay in
					sync: check one off here and its card moves to Done on the board; rename the task on the board
					and the bullet follows.`))),
				bullets(
					item(text("The "), bold("Task Board"), text(" in the sidebar shows every task you can see, from every note.")),
					item(text("Folders have boards of their own: open "), bold("Product Launch"), text(" or "), bold("Engineering"), text(" and switch to the board view.")),
					item(text("Press "), code("⌘K"), text(" / "), code("Ctrl+K"), text(" to search notes and tasks.")),
				),
				heading(2, text("TODO")),
				bullets(todo(0), todo(1), todo(2), todo(3)),
				heading(2, text("Further reading")),
				bullets(
					item(link("ProseMirror guide", "https://prosemirror.net/docs/guide/"), text(" — the editor framework under the hood")),
					item(link("Markdown basics", "https://www.markdownguide.org/basic-syntax/"), text(" — most Markdown shortcuts work while typing")),
				),
			)
		},
	}
}

func productLaunchFolder() treeNode {
	return treeNode{
		folder:   true,
		title:    "Product Launch",
		order:    1,
		tags:     []string{"launch"},
		priority: model.PriorityHigh,
		boardTasks: []taskSpec{
			{title: "Confirm launch date with leadership", status: model.StatusTodo, priority: model.PriorityUrgent,
				tags: []string{"launch"}, due: days(1),
				description: "Need a go/no-go by Friday so marketing can lock the announcement schedule."},
			{title: "Collect beta feedback summary", status: model.StatusInProgress, priority: model.PriorityMedium,
				tags: []string{"launch", "research"}, due: days(6),
				link: &model.LinkMeta{URL: "https://example.com/beta-feedback", Title: strp("Beta feedback tracker"),
					Description: strp("All feedback from the private beta, grouped by theme."), SiteName: strp("example.com")}},
		},
		lanes: []laneSpec{
			{title: "To Do", order: 0, filter: rules(model.ConjunctionAnd,
				fr("seed-pl-1", "status", model.FilterOpEq, "todo")), sort: autoSort()},
			{title: "In Progress", order: 1, filter: rules(model.ConjunctionAnd,
				fr("seed-pl-2", "status", model.FilterOpEq, "in-progress")), sort: autoSort()},
			{title: "Urgent & High", order: 2, filter: rules(model.ConjunctionAnd,
				fr("seed-pl-3", "priority", model.FilterOpIn, []string{"urgent", "high"}),
				fr("seed-pl-4", "status", model.FilterOpIn, []string{"todo", "in-progress"})), sort: fieldSort("dueDate", model.SortDirectionAsc)},
			{title: "Done", order: 3, filter: rules(model.ConjunctionAnd,
				fr("seed-pl-5", "status", model.FilterOpIn, []string{"done", "cancelled"})), sort: fieldSort("updatedAt", model.SortDirectionDesc)},
		},
		children: []treeNode{launchPlanNote(), marketingBriefNote()},
	}
}

func launchPlanNote() treeNode {
	return treeNode{
		title:    "Launch Plan",
		order:    1,
		tags:     []string{"launch", "planning"},
		priority: model.PriorityHigh,
		tasks: []taskSpec{
			{title: "Run load test against staging", status: model.StatusTodo, priority: model.PriorityUrgent,
				tags: []string{"launch", "engineering"}, due: days(2),
				description: "Target: 500 concurrent editors on one folder board, p95 under 300 ms."},
			{title: "Finalize pricing page copy", status: model.StatusInProgress, priority: model.PriorityHigh,
				tags: []string{"launch", "marketing"}, due: days(3)},
			{title: "Record product walkthrough video", status: model.StatusTodo, priority: model.PriorityMedium,
				tags: []string{"launch", "marketing"}, due: days(7)},
			{title: "Draft launch announcement email", status: model.StatusDone, priority: model.PriorityMedium,
				tags: []string{"marketing"}, due: days(-2)},
			{title: "Book a launch-day war room", status: model.StatusCancelled, priority: model.PriorityLow,
				tags: []string{"launch"}},
		},
		content: func(todo todoFn) pmNode {
			return doc(
				paragraph(text(trimLines(`Glyph 2.0 brings folder boards and realtime collaboration to every workspace.
					This note is the single source of truth for the launch: timeline, risks and who is doing what.`))),
				heading(2, text("Timeline")),
				table(
					[]string{"Milestone", "Target", "Owner"},
					[]string{"Feature freeze", "Week 1", "Engineering"},
					[]string{"Beta feedback review", "Week 2", "Product"},
					[]string{"Public announcement", "Week 3", "Marketing"},
				),
				heading(2, text("Risks")),
				bullets(
					item(bold("Performance:"), text(" large folder boards have not been load tested yet.")),
					item(bold("Messaging:"), text(" pricing copy still needs sign-off.")),
				),
				quote(paragraph(italic("Ship the smallest thing that proves the idea, then listen."))),
				heading(2, text("TODO")),
				bullets(todo(0), todo(1), todo(2), todo(3), todo(4)),
			)
		},
	}
}

func marketingBriefNote() treeNode {
	return treeNode{
		title:    "Marketing Brief",
		order:    2,
		tags:     []string{"marketing"},
		priority: model.PriorityMedium,
		tasks: []taskSpec{
			{title: "Write three customer quotes for the landing page", status: model.StatusTodo, priority: model.PriorityHigh,
				tags: []string{"marketing"}, due: days(5)},
			{title: "Prepare the social media schedule", status: model.StatusInProgress, priority: model.PriorityMedium,
				tags: []string{"marketing", "social"}, due: days(4)},
			{title: "Update press kit screenshots", status: model.StatusTodo, priority: model.PriorityLow,
				tags: []string{"marketing"}},
		},
		content: func(todo todoFn) pmNode {
			return doc(
				heading(2, text("Audience")),
				numbered(
					item(text("Small teams who keep notes and tasks in separate tools")),
					item(text("Engineering leads running weekly planning")),
					item(text("Anyone tired of copying action items out of meeting notes")),
				),
				heading(2, text("Key messages")),
				bullets(
					item(bold("Write, don't file."), text(" Tasks come straight out of your notes.")),
					item(bold("One board per project."), text(" Every folder gets its own kanban board.")),
					item(bold("Together, live."), text(" Edit the same note at the same time.")),
				),
				heading(2, text("TODO")),
				bullets(todo(0), todo(1), todo(2)),
			)
		},
	}
}

func engineeringFolder() treeNode {
	return treeNode{
		folder: true,
		title:  "Engineering",
		order:  2,
		tags:   []string{"engineering"},
		boardTasks: []taskSpec{
			{title: "Rotate staging database credentials", status: model.StatusTodo, priority: model.PriorityHigh,
				tags: []string{"ops", "security"}, due: days(-1),
				description: "Overdue: the old credentials expire at the end of the week."},
		},
		lanes: []laneSpec{
			{title: "Sprint 14", order: 0, filter: rules(model.ConjunctionAnd,
				fr("seed-en-1", "tags", model.FilterOpContains, "sprint-14"),
				fr("seed-en-2", "status", model.FilterOpIn, []string{"todo", "in-progress"})), sort: autoSort()},
			{title: "Bugs", order: 1, filter: rules(model.ConjunctionAnd,
				fr("seed-en-3", "tags", model.FilterOpContains, "bug"),
				fr("seed-en-4", "status", model.FilterOpNeq, "done")), sort: autoSort()},
			{title: "In Progress", order: 2, filter: rules(model.ConjunctionAnd,
				fr("seed-en-5", "status", model.FilterOpEq, "in-progress")), sort: autoSort()},
			{title: "Done", order: 3, filter: rules(model.ConjunctionAnd,
				fr("seed-en-6", "status", model.FilterOpEq, "done")), sort: fieldSort("updatedAt", model.SortDirectionDesc)},
		},
		children: []treeNode{sprintPlanningNote(), architectureNote(), meetingsFolder()},
	}
}

func sprintPlanningNote() treeNode {
	return treeNode{
		title:    "Sprint 14 Planning",
		order:    1,
		tags:     []string{"sprint-14", "planning"},
		priority: model.PriorityMedium,
		tasks: []taskSpec{
			{title: "Fix sidebar flicker when renaming a folder", status: model.StatusInProgress, priority: model.PriorityHigh,
				tags: []string{"bug", "frontend", "sprint-14"}, due: days(2)},
			{title: "Investigate slow board load for large folders", status: model.StatusTodo, priority: model.PriorityHigh,
				tags: []string{"performance", "sprint-14"}, due: days(4),
				description: "Boards with 1,000+ tasks take several seconds to render. Profile the lane filter first."},
			{title: "Add pagination to the search API", status: model.StatusTodo, priority: model.PriorityMedium,
				tags: []string{"backend", "sprint-14"}, due: days(8)},
			{title: "Write migration for archived tasks", status: model.StatusTodo, priority: model.PriorityMedium,
				tags: []string{"backend", "sprint-14"}, due: days(10)},
			{title: "Upgrade the Go toolchain", status: model.StatusDone, priority: model.PriorityLow,
				tags: []string{"chore", "sprint-14"}},
		},
		content: func(todo todoFn) pmNode {
			return doc(
				heading(2, text("Sprint goal")),
				paragraph(text(trimLines(`Make large folder boards fast and fix the paper cuts reported during the beta.`))),
				heading(2, text("Capacity")),
				bullets(
					item(text("Backend: 2 engineers, full sprint")),
					item(text("Frontend: 1 engineer, out on Thursday")),
					item(text("On-call rotation takes ~20% of one engineer")),
				),
				heading(2, text("TODO")),
				bullets(todo(0), todo(1), todo(2), todo(3), todo(4)),
				heading(2, text("Out of scope")),
				paragraph(italic("Offline mode and the public API are next quarter.")),
			)
		},
	}
}

func architectureNote() treeNode {
	return treeNode{
		title: "Architecture Notes",
		order: 2,
		tags:  []string{"architecture", "reference"},
		content: func(todoFn) pmNode {
			return doc(
				paragraph(text("A quick map of the system for new team members.")),
				heading(2, text("Components")),
				bullets(
					item(bold("Web app"), text(" — SvelteKit, with a TipTap (ProseMirror) editor.")),
					item(bold("API"), text(" — Go and Gin, backed by Postgres.")),
					item(bold("Collab service"), text(" — Yjs over WebSockets for realtime editing.")),
				),
				heading(2, text("How a TODO bullet becomes a task")),
				numbered(
					item(text("The editor spots a new bullet under a "), code("TODO"), text(" heading.")),
					item(text("It creates the task with "), code("POST /api/v1/tasks"), text(", passing the bullet's id.")),
					item(text("It stores the task id on the bullet, so the two stay linked.")),
				),
				codeBlock("sql", "SELECT title, status, due_date\nFROM tasks\nWHERE source_page_id = $1 AND deleted_at IS NULL\nORDER BY \"order\";"),
				rule(),
				paragraph(text("Questions? Ask in "), code("#eng-glyph"), text(".")),
			)
		},
	}
}

func meetingsFolder() treeNode {
	return treeNode{
		folder: true,
		title:  "Meetings",
		order:  3,
		children: []treeNode{{
			title: "Weekly Engineering Sync",
			order: 1,
			tags:  []string{"meeting"},
			tasks: []taskSpec{
				{title: "Follow up on flaky end-to-end tests", status: model.StatusTodo, priority: model.PriorityHigh,
					tags: []string{"bug", "testing"}, due: days(-1)},
				{title: "Share the retro notes with the team", status: model.StatusDone, priority: model.PriorityLow,
					tags: []string{"meeting"}},
				{title: "Schedule the architecture review", status: model.StatusTodo, priority: model.PriorityNone,
					tags: []string{"meeting"}},
			},
			content: func(todo todoFn) pmNode {
				return doc(
					heading(2, text("Attendees")),
					bullets(item(text("Priya")), item(text("Marcus")), item(text("Lena")), item(text("Sam"))),
					heading(2, text("Notes")),
					bullets(
						item(text("Beta usage is up 40% week over week.")),
						item(text("Two flaky e2e tests blocked merges on Tuesday.")),
						item(text("Load testing moves to next week; staging is being resized.")),
					),
					heading(2, text("TODO")),
					bullets(todo(0), todo(1), todo(2)),
				)
			},
		}},
	}
}

// demoTemplates are the organization's note templates. "Default" mirrors
// the template the web app creates for a new user, who would otherwise get
// none (the app only seeds one when no templates are visible).
func demoTemplates() []templateSpec {
	return []templateSpec{
		{
			name:      "Default",
			isDefault: true,
			content:   doc(heading(1, text("TODO"))),
		},
		{
			name:          "Meeting Notes",
			titleTemplate: "Meeting — {{date-long}}",
			content: doc(
				heading(2, text("Attendees")),
				bullets(item()),
				heading(2, text("Agenda")),
				numbered(item()),
				heading(2, text("Notes")),
				paragraph(),
				heading(2, text("TODO")),
				paragraph(),
			),
		},
		{
			name:          "Weekly Review",
			titleTemplate: "Weekly Review {{week}}",
			content: doc(
				heading(2, text("Wins")),
				bullets(item()),
				heading(2, text("Challenges")),
				bullets(item()),
				heading(2, text("TODO")),
				paragraph(),
			),
		},
	}
}
