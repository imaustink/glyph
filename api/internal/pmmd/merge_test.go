package pmmd_test

import (
	"encoding/json"
	"testing"

	"github.com/glyph/api/internal/pmmd"
)

// listItems returns every listItem's attrs and first-paragraph text, in doc
// order.
type itemInfo struct {
	attrs map[string]any
	text  string
}

func listItems(t *testing.T, raw []byte) []itemInfo {
	t.Helper()
	var out []itemInfo
	var walk func(v any)
	walk = func(v any) {
		n, ok := v.(map[string]any)
		if !ok {
			return
		}
		if n["type"] == "listItem" {
			a, _ := n["attrs"].(map[string]any)
			text := ""
			if c, _ := n["content"].([]any); len(c) > 0 {
				text = plainText(c[0])
			}
			out = append(out, itemInfo{attrs: a, text: text})
		}
		if c, ok := n["content"].([]any); ok {
			for _, x := range c {
				walk(x)
			}
		}
	}
	walk(decode(t, raw))
	return out
}

func plainText(v any) string {
	n, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if s, ok := n["text"].(string); ok {
		return s
	}
	s := ""
	if c, ok := n["content"].([]any); ok {
		for _, x := range c {
			s += plainText(x)
		}
	}
	return s
}

// replaceVia simulates an agent's replace: read prev as Markdown, edit it,
// parse the edit, and merge it against prev.
func replaceVia(t *testing.T, prev string, edit func(md string) string) json.RawMessage {
	t.Helper()
	md, err := pmmd.ToMarkdown(json.RawMessage(prev))
	if err != nil {
		t.Fatal(err)
	}
	next := fromMD(t, edit(md))
	merged, err := pmmd.MergeReplace(json.RawMessage(prev), next)
	if err != nil {
		t.Fatal(err)
	}
	assertValid(t, merged)
	return merged
}

// TestMergeReplaceKeepsWhatMarkdownCannotExpress: a replace that leaves a
// block unchanged must keep it exactly — underline, link target/rel/title,
// orderedList type, nodeIds — even though Markdown can't carry any of it
// (DI-08).
func TestMergeReplaceKeepsWhatMarkdownCannotExpress(t *testing.T) {
	underlined := p(txt("under", mk("underline")), txt(" and "),
		`{"type":"text","text":"site","marks":[{"type":"link","attrs":{"href":"https://ex.com","target":"_blank","rel":"noopener","title":"Ex"}}]}`)
	typedList := `{"type":"orderedList","attrs":{"start":1,"type":"a"},"content":[` +
		`{"type":"listItem","attrs":{"nodeId":"o1"},"content":[` + p(txt("alpha")) + `]}]}`
	prev := doc(underlined, typedList, h(2, txt("Other")), p(txt("will change")))

	merged := replaceVia(t, prev, func(md string) string {
		return md[:len(md)-len("will change\n")] + "changed\n"
	})

	got := decode(t, merged).(map[string]any)["content"].([]any)
	want := decode(t, []byte(prev)).(map[string]any)["content"].([]any)
	for i := 0; i < 3; i++ {
		if mustJSON(t, got[i]) != mustJSON(t, want[i]) {
			t.Fatalf("unchanged block %d not kept\nwant: %s\ngot:  %s", i, mustJSON(t, want[i]), mustJSON(t, got[i]))
		}
	}
	if plainText(got[3]) != "changed" {
		t.Fatalf("edited block should take the new text: %s", mustJSON(t, got[3]))
	}
}

// TestMergeReplaceCarriesNodeIDs: unchanged bullets keep their nodeId, an
// edited bullet keeps the nodeId of the bullet it replaced (same position),
// and a new bullet gets a fresh one — instead of every bullet getting a new
// nodeId on every replace (DI-08).
func TestMergeReplaceCarriesNodeIDs(t *testing.T) {
	prev := doc(ul(
		`{"type":"listItem","attrs":{"nodeId":"n1"},"content":[`+p(txt("a"))+`]}`,
		`{"type":"listItem","attrs":{"nodeId":"n2"},"content":[`+p(txt("b"))+`]}`,
	))
	merged := replaceVia(t, prev, func(string) string { return "- a\n- b edited\n- c\n" })
	items := listItems(t, merged)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if items[0].attrs["nodeId"] != "n1" {
		t.Errorf("unchanged bullet lost its nodeId: %v", items[0].attrs)
	}
	if items[1].attrs["nodeId"] != "n2" {
		t.Errorf("edited bullet should keep the nodeId at its position: %v", items[1].attrs)
	}
	if id := items[2].attrs["nodeId"]; id == "n1" || id == "n2" || id == nil {
		t.Errorf("new bullet needs a fresh nodeId: %v", items[2].attrs)
	}
}

// TestMergeReplaceNeverDuplicatesNodeIDs: a bullet repeated in the edit gets
// its old nodeId only once.
func TestMergeReplaceNeverDuplicatesNodeIDs(t *testing.T) {
	prev := doc(ul(`{"type":"listItem","attrs":{"nodeId":"n1"},"content":[` + p(txt("a")) + `]}`))
	merged := replaceVia(t, prev, func(string) string { return "- a\n- a\n" })
	items := listItems(t, merged)
	if len(items) != 2 || items[0].attrs["nodeId"] == items[1].attrs["nodeId"] {
		t.Fatalf("duplicate nodeIds: %v", items)
	}
}

// TestSanitizeTaskLinks: a taskId repeated in the doc, or naming a task that
// isn't this page's, is dropped from the bullet (DI-08: two bullets editing
// one task; a dangling link to another page's task). Repeated nodeIds are
// re-issued.
func TestSanitizeTaskLinks(t *testing.T) {
	in := doc(ul(
		`{"type":"listItem","attrs":{"nodeId":"n1","taskId":"`+taskA+`","taskStatus":"todo","checked":false},"content":[`+p(txt("mine"))+`]}`,
		`{"type":"listItem","attrs":{"nodeId":"n1","taskId":"`+taskA+`","taskStatus":"todo","checked":false},"content":[`+p(txt("dup"))+`]}`,
		`{"type":"listItem","attrs":{"nodeId":"n3","taskId":"`+taskB+`","taskStatus":"done","checked":true},"content":[`+p(txt("foreign"))+`]}`,
	))
	out, dropped, err := pmmd.SanitizeTaskLinks(json.RawMessage(in), func(id string) bool { return id == taskA })
	if err != nil {
		t.Fatal(err)
	}
	assertValid(t, out)
	items := listItems(t, out)
	if items[0].attrs["taskId"] != taskA || items[0].attrs["nodeId"] != "n1" {
		t.Errorf("first link must stay: %v", items[0].attrs)
	}
	if _, ok := items[1].attrs["taskId"]; ok {
		t.Errorf("duplicate taskId must be dropped: %v", items[1].attrs)
	}
	if items[1].attrs["nodeId"] == "n1" {
		t.Errorf("duplicate nodeId must be re-issued: %v", items[1].attrs)
	}
	if _, ok := items[2].attrs["taskId"]; ok {
		t.Errorf("foreign taskId must be dropped: %v", items[2].attrs)
	}
	if _, ok := items[2].attrs["taskStatus"]; ok {
		t.Errorf("foreign taskStatus must be dropped: %v", items[2].attrs)
	}
	if len(dropped) != 2 {
		t.Errorf("want 2 dropped links, got %v", dropped)
	}
}

// TestApplyTaskStatuses: the checkbox a bullet shows comes from its task row,
// not the attribute stored in the doc, which goes stale when the task is
// changed outside the editor.
func TestApplyTaskStatuses(t *testing.T) {
	in := doc(ul(
		task(taskA, "todo", false, p(txt("finished elsewhere"))),
		task(taskB, "done", true, p(txt("reopened elsewhere"))),
		task(taskC, "todo", false, p(txt("unknown task"))),
	))
	out, err := pmmd.ApplyTaskStatuses(json.RawMessage(in), map[string]string{taskA: "done", taskB: "in-progress"})
	if err != nil {
		t.Fatal(err)
	}
	md, err := pmmd.ToMarkdown(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "- [x] finished elsewhere <!-- task:" + taskA + " -->\n" +
		"- [ ] reopened elsewhere <!-- task:" + taskB + " -->\n" +
		"- [ ] unknown task <!-- task:" + taskC + " -->\n"
	if md != want {
		t.Fatalf("got %q\nwant %q", md, want)
	}
	items := listItems(t, out)
	if items[0].attrs["checked"] != true || items[1].attrs["checked"] != false {
		t.Fatalf("checked must follow the status: %v", items)
	}
}

func TestListItemNodeIDsAndTaskIDs(t *testing.T) {
	in := doc(ul(
		task(taskA, "todo", false, p(txt("a")), ul(`{"type":"listItem","attrs":{"nodeId":"child"},"content":[`+p(txt("c"))+`]}`)),
		`{"type":"listItem","content":[`+p(txt("no id"))+`]}`,
	))
	ids, err := pmmd.ListItemNodeIDs(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	if !ids["child"] || len(ids) != 2 {
		t.Fatalf("nodeIds = %v", ids)
	}
	tasks, err := pmmd.TaskIDs(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0] != taskA {
		t.Fatalf("taskIds = %v", tasks)
	}
}
