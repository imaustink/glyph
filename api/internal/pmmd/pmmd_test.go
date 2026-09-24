package pmmd_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/glyph/api/internal/handler"
	"github.com/glyph/api/internal/pmmd"
)

// ---- helpers ----

func decode(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("invalid JSON %s: %v", raw, err)
	}
	return v
}

// normalize strips generated / default-valued attributes so documents can be
// compared structurally: nodeId is dropped, checked:false is dropped, empty
// attrs removed, marks sorted by type.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			out[k] = normalize(val)
		}
		if attrs, ok := out["attrs"].(map[string]any); ok {
			delete(attrs, "nodeId")
			if c, ok := attrs["checked"].(bool); ok && !c {
				delete(attrs, "checked")
			}
			if len(attrs) == 0 {
				delete(out, "attrs")
			}
		}
		if marks, ok := out["marks"].([]any); ok {
			sort.SliceStable(marks, func(i, j int) bool {
				return marks[i].(map[string]any)["type"].(string) < marks[j].(map[string]any)["type"].(string)
			})
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertSameDoc(t *testing.T, want, got []byte) {
	t.Helper()
	w, g := normalize(decode(t, want)), normalize(decode(t, got))
	if !reflect.DeepEqual(w, g) {
		t.Fatalf("documents differ\nwant: %s\ngot:  %s", mustJSON(t, w), mustJSON(t, g))
	}
}

// assertValid checks that the server validator accepts doc without stripping
// anything.
func assertValid(t *testing.T, doc []byte) {
	t.Helper()
	sanitized, err := handler.ValidateProseMirrorContent(doc)
	if err != nil {
		t.Fatalf("validator rejected %s: %v", doc, err)
	}
	if !reflect.DeepEqual(decode(t, doc), decode(t, sanitized)) {
		t.Fatalf("validator altered doc\nin:  %s\nout: %s", doc, sanitized)
	}
	assertNoEmptyText(t, decode(t, doc))
}

func assertNoEmptyText(t *testing.T, v any) {
	t.Helper()
	switch n := v.(type) {
	case map[string]any:
		if n["type"] == "text" {
			if s, _ := n["text"].(string); s == "" {
				t.Fatalf("empty text node found")
			}
		}
		if c, ok := n["content"]; ok {
			if arr, _ := c.([]any); len(arr) == 0 && n["type"] != "doc" {
				t.Fatalf("node %v has empty content array", n["type"])
			}
		}
		for _, c := range n {
			assertNoEmptyText(t, c)
		}
	case []any:
		for _, c := range n {
			assertNoEmptyText(t, c)
		}
	}
}

func fromMD(t *testing.T, md string) json.RawMessage {
	t.Helper()
	pm, err := pmmd.FromMarkdown(md)
	if err != nil {
		t.Fatalf("FromMarkdown: %v", err)
	}
	assertValid(t, pm)
	return pm
}

func toMD(t *testing.T, doc string) string {
	t.Helper()
	md, err := pmmd.ToMarkdown(json.RawMessage(doc))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	return md
}

// small builders for readable docs
func doc(blocks ...string) string {
	return `{"type":"doc","content":[` + strings.Join(blocks, ",") + `]}`
}
func p(inl ...string) string {
	if len(inl) == 0 {
		return `{"type":"paragraph"}`
	}
	return `{"type":"paragraph","content":[` + strings.Join(inl, ",") + `]}`
}
func txt(s string, marks ...string) string {
	b, _ := json.Marshal(s)
	if len(marks) == 0 {
		return `{"type":"text","text":` + string(b) + `}`
	}
	return `{"type":"text","marks":[` + strings.Join(marks, ",") + `],"text":` + string(b) + `}`
}
func mk(typ string) string { return `{"type":"` + typ + `"}` }
func link(href string) string {
	return `{"type":"link","attrs":{"href":"` + href + `"}}`
}
func h(level int, inl ...string) string {
	return `{"type":"heading","attrs":{"level":` + string(rune('0'+level)) + `},"content":[` + strings.Join(inl, ",") + `]}`
}
func ul(items ...string) string {
	return `{"type":"bulletList","content":[` + strings.Join(items, ",") + `]}`
}
func ol(items ...string) string {
	return `{"type":"orderedList","content":[` + strings.Join(items, ",") + `]}`
}
func li(blocks ...string) string {
	return `{"type":"listItem","attrs":{"nodeId":"` + uuid.NewString() + `"},"content":[` + strings.Join(blocks, ",") + `]}`
}
func task(id, status string, checked bool, blocks ...string) string {
	c := "false"
	if checked {
		c = "true"
	}
	return `{"type":"listItem","attrs":{"nodeId":"` + uuid.NewString() + `","taskId":"` + id + `","taskStatus":"` + status + `","checked":` + c + `},"content":[` + strings.Join(blocks, ",") + `]}`
}
func quote(blocks ...string) string {
	return `{"type":"blockquote","content":[` + strings.Join(blocks, ",") + `]}`
}
func code(lang, text string) string {
	b, _ := json.Marshal(text)
	return `{"type":"codeBlock","attrs":{"language":"` + lang + `"},"content":[{"type":"text","text":` + string(b) + `}]}`
}

const (
	taskA = "1b4e28ba-2d1e-4a3b-9d9b-3c1f6f6c0e2a"
	taskB = "6f1c2c1e-8a41-4a8e-9b53-2f0d7e3e9a10"
	taskC = "0f8fad5b-d9cb-469f-a165-70867728950e"
)

// ---- FromMarkdown ----

func TestFromMarkdownStructure(t *testing.T) {
	cases := []struct {
		name, md, want string
	}{
		{"empty", "", doc(p())},
		{"whitespace only", "  \n\n\t\n", doc(p())},
		{"headings", "# One\n###### Six ##\n####### seven",
			doc(h(1, txt("One")), h(6, txt("Six")), p(txt("####### seven")))},
		{"soft break joins with space", "one\ntwo\n  three", doc(p(txt("one two three")))},
		{"hard breaks", "a  \nb\\\nc", doc(p(txt("a"), mk("hardBreak"), txt("b"), mk("hardBreak"), txt("c")))},
		{"thematic breaks", "---\n***\n_ _ _", doc(mk("horizontalRule"), mk("horizontalRule"), mk("horizontalRule"))},
		{"fenced backticks", "```js\nconst a = 1;\n\n# no\n```", doc(code("js", "const a = 1;\n\n# no"))},
		{"fenced tildes", "~~~python extra\nx\n~~~", doc(code("python", "x"))},
		{"fence without language", "```\n- not a list\n```",
			doc(`{"type":"codeBlock","content":[{"type":"text","text":"- not a list"}]}`)},
		{"empty fence", "```\n```", doc(`{"type":"codeBlock"}`)},
		{"blockquote recursive", "> a\n> > b\n>\n> - c",
			doc(quote(p(txt("a")), quote(p(txt("b"))), ul(li(p(txt("c"))))))},
		{"bullets any marker", "- a\n- b", doc(ul(li(p(txt("a"))), li(p(txt("b")))))},
		{"marker change starts new list", "- a\n* b", doc(ul(li(p(txt("a")))), ul(li(p(txt("b")))))},
		{"ordered", "1. a\n2) b\n3) c", doc(ol(li(p(txt("a")))), ol(li(p(txt("b"))), li(p(txt("c")))))},
		{"loose list", "- a\n\n- b\n\n  second para", doc(ul(li(p(txt("a"))), li(p(txt("b")), p(txt("second para")))))},
		{"nested 3 deep", "- a\n  - b\n    - c\n- d",
			doc(ul(li(p(txt("a")), ul(li(p(txt("b")), ul(li(p(txt("c"))))))), li(p(txt("d")))))},
		{"nested with 4-space indent", "1. a\n    - b\n        - c",
			doc(ol(li(p(txt("a")), ul(li(p(txt("b")), ul(li(p(txt("c")))))))))},
		{"lazy continuation", "- a\nstill a", doc(ul(li(p(txt("a still a")))))},
		{"task items", "- [ ] open\n- [x] closed\n- [X] closed too",
			doc(ul(
				`{"type":"listItem","attrs":{"checked":false},"content":[`+p(txt("open"))+`]}`,
				`{"type":"listItem","attrs":{"checked":true},"content":[`+p(txt("closed"))+`]}`,
				`{"type":"listItem","attrs":{"checked":true},"content":[`+p(txt("closed too"))+`]}`))},
		{"empty item", "-\n- b", doc(ul(li(p()), li(p(txt("b")))))},
		{"image line", `![A cat](https://ex.com/c.png "Title")`,
			doc(`{"type":"image","attrs":{"src":"https://ex.com/c.png","alt":"A cat","title":"Title"}}`)},
		{"image interrupts paragraph", "text\n![a](/x.png)\nmore",
			doc(p(txt("text")), `{"type":"image","attrs":{"src":"/x.png","alt":"a"}}`, p(txt("more")))},
		{"inline image becomes linked alt", "see ![pic](https://ex.com/p.png) here",
			doc(p(txt("see "), txt("pic", link("https://ex.com/p.png")), txt(" here")))},
		{"list after paragraph without blank", "intro\n- a", doc(p(txt("intro")), ul(li(p(txt("a")))))},
		{"heading first in item gets empty paragraph", "- # h", doc(ul(li(p(), h(1, txt("h")))))},
		{"html comment dropped", "a <!-- note --> b", doc(p(txt("a  b")))},
		{"crlf", "# a\r\n\r\nb\r\n", doc(h(1, txt("a")), p(txt("b")))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fromMD(t, tc.md)
			assertSameDoc(t, []byte(tc.want), got)
		})
	}
}

func TestFromMarkdownInline(t *testing.T) {
	b, i, s, c := mk("bold"), mk("italic"), mk("strike"), mk("code")
	cases := []struct {
		name, md string
		want     []string
	}{
		{"bold stars", "**x**", []string{txt("x", b)}},
		{"bold underscores", "__x__", []string{txt("x", b)}},
		{"italic star", "*x*", []string{txt("x", i)}},
		{"italic underscore", "_x_", []string{txt("x", i)}},
		{"intraword underscore", "snake_case_name", []string{txt("snake_case_name")}},
		{"intraword star", "un*frigging*believable", []string{txt("un"), txt("frigging", i), txt("believable")}},
		{"strike", "~~x~~ ~y~", []string{txt("x", s), txt(" ~y~")}},
		{"code", "`a *b*`", []string{txt("a *b*", c)}},
		{"double backtick code", "`` a`b ``", []string{txt("a`b", c)}},
		{"unclosed code", "`abc", []string{txt("`abc")}},
		{"bold italic", "***x***", []string{txt("x", b, i)}},
		{"nested emphasis", "**a *b* c**", []string{txt("a ", b), txt("b", b, i), txt(" c", b)}},
		{"link", "[t](https://ex.com)", []string{txt("t", link("https://ex.com"))}},
		{"link bold text", "[**bold** plain](https://ex.com)", []string{txt("bold", b, link("https://ex.com")), txt(" plain", link("https://ex.com"))}},
		{"bold around link", "**[t](/rel)**", []string{txt("t", b, link("/rel"))}},
		{"link with title and parens", `[t](https://ex.com/a_(b) "T")`, []string{txt("t", link("https://ex.com/a_(b)"))}},
		{"angle dest", "[t](<https://ex.com/a b>)", []string{txt("t", link("https://ex.com/a b"))}},
		{"mailto", "[m](mailto:a@b.co)", []string{txt("m", link("mailto:a@b.co"))}},
		{"autolink", "<https://ex.com/x>", []string{txt("https://ex.com/x", link("https://ex.com/x"))}},
		{"email autolink", "<a@b.co>", []string{txt("a@b.co", link("mailto:a@b.co"))}},
		{"escapes", `\*not\* \_x\_ \# \[y\] \\ \q`, []string{txt(`*not* _x_ # [y] \ \q`)}},
		{"not a link", "[a] (b)", []string{txt("[a] (b)")}},
		{"unmatched delims", "a * b ** c", []string{txt("a * b ** c")}},
		{"code in link", "[`x`](/y)", []string{txt("x", c, link("/y"))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fromMD(t, tc.md)
			assertSameDoc(t, []byte(doc(p(tc.want...))), got)
		})
	}
}

func TestMaliciousHrefRejected(t *testing.T) {
	cases := []string{
		"[click](javascript:alert(1))",
		"[click](JaVaScRiPt:alert(1))",
		"[click](<java\tscript:alert(1)>)",
		"[click](data:text/html;base64,xx)",
		"[click](vbscript:msgbox)",
		"<javascript:alert(1)>",
		"![x](javascript:alert(1))",
		"text ![x](javascript:alert(1)) text",
	}
	for _, md := range cases {
		t.Run(md, func(t *testing.T) {
			got := string(fromMD(t, md))
			low := strings.ToLower(got)
			for _, bad := range []string{`"href":"javascript`, `"href":"data`, `"href":"vbscript`, `"src":"javascript`, `"href":"java`} {
				if strings.Contains(low, bad) {
					t.Fatalf("unsafe URL survived: %s", got)
				}
			}
			if strings.Contains(got, `"type":"image"`) {
				t.Fatalf("unsafe image kept: %s", got)
			}
		})
	}
	// Label text is kept, just not linked.
	got := fromMD(t, "[click](javascript:alert(1))")
	assertSameDoc(t, []byte(doc(p(txt("click")))), got)
}

func TestFreshNodeIDs(t *testing.T) {
	got := fromMD(t, "- a\n- b\n  - c\n1. d")
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["type"] == "listItem" {
				attrs, _ := n["attrs"].(map[string]any)
				id, _ := attrs["nodeId"].(string)
				if _, err := uuid.Parse(id); err != nil {
					t.Fatalf("listItem without valid nodeId: %v", attrs)
				}
				if seen[id] {
					t.Fatalf("duplicate nodeId %s", id)
				}
				seen[id] = true
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(decode(t, got))
	if len(seen) != 4 {
		t.Fatalf("expected 4 list items, got %d", len(seen))
	}
}

// ---- ToMarkdown ----

func TestToMarkdown(t *testing.T) {
	cases := []struct {
		name, doc, want string
	}{
		{"empty doc", doc(), ""},
		{"empty paragraph doc", doc(p()), ""},
		{"headings and paragraphs", doc(h(1, txt("T")), p(txt("a")), h(3, txt("x#"))), "# T\n\na\n\n### x\\#\n"},
		{"marks", doc(p(txt("b", mk("bold")), txt(" "), txt("i", mk("italic")), txt(" "), txt("s", mk("strike")), txt(" "), txt("c", mk("code")))),
			"**b** *i* ~~s~~ `c`\n"},
		{"overlapping marks", doc(p(txt("a", mk("bold")), txt("b", mk("bold"), mk("italic")), txt("c", mk("italic")))),
			"**a*b***_c_\n"},
		{"whitespace expelled", doc(p(txt("x"), txt(" bold ", mk("bold")), txt("y"))), "x **bold** y\n"},
		{"plain-text marks", doc(p(txt("u", mk("underline")), txt("h", mk("highlight")), txt("s", mk("subscript")), txt("S", mk("superscript")))),
			"uhsS\n"},
		{"link bold", doc(p(txt("bold", mk("bold"), link("https://ex.com")), txt(" rest", link("https://ex.com")))),
			"[**bold** rest](https://ex.com)\n"},
		{"code with backticks", doc(p(txt("a`b", mk("code")), txt(" "), txt("`x", mk("code")))), "``a`b`` `` `x ``\n"},
		{"hard break", doc(p(txt("a"), mk("hardBreak"), txt("b"), mk("hardBreak"))), "a\\\nb\n"},
		{"blockquote", doc(quote(p(txt("a")), p(txt("b")))), "> a\n>\n> b\n"},
		{"code block", doc(code("go", "```\nx")), "````go\n```\nx\n````\n"},
		{"hr", doc(p(txt("a")), mk("horizontalRule")), "a\n\n---\n"},
		{"image", doc(`{"type":"image","attrs":{"src":"https://ex.com/a b.png","alt":"A [x]","title":"say \"hi\""}}`),
			"![A \\[x\\]](<https://ex.com/a b.png> \"say \\\"hi\\\"\")\n"},
		{"ordered start", doc(`{"type":"orderedList","attrs":{"start":9},"content":[` + li(p(txt("a"))) + `,` + li(p(txt("b"))) + `]}`),
			"9. a\n10. b\n"},
		{"nested lists", doc(ul(li(p(txt("a")), ol(li(p(txt("b")), ul(li(p(txt("c"))))))))),
			"- a\n  1. b\n     - c\n"},
		{"loose item", doc(ul(li(p(txt("a")), p(txt("b"))), li(p(txt("c"))))), "- a\n\n  b\n\n- c\n"},
		{"adjacent lists", doc(ul(li(p(txt("a")))), ul(li(p(txt("b")))), ol(li(p(txt("c")))), ol(li(p(txt("d"))))),
			"- a\n\n* b\n\n1. c\n\n1) d\n"},
		{"tasks", doc(ul(
			task(taskA, "todo", false, p(txt("t1"))),
			task(taskB, "done", true, p(txt("t2"))),
			task(taskC, "in-progress", false, p(txt("t3"))),
			task(taskA, "cancelled", true, p(txt("t4"))),
			`{"type":"listItem","attrs":{"checked":true},"content":[`+p(txt("plain checked"))+`]}`,
			`{"type":"listItem","attrs":{"checked":false,"taskId":null},"content":[`+p(txt("plain"))+`]}`,
		)),
			"- [ ] t1 <!-- task:" + taskA + " -->\n" +
				"- [x] t2 <!-- task:" + taskB + " -->\n" +
				"- [ ] t3 <!-- task:" + taskC + " -->\n" +
				"- [x] t4 <!-- task:" + taskA + " -->\n" +
				"- [x] plain checked\n" +
				"- plain\n"},
		{"empty task item", doc(ul(task(taskA, "todo", false, p()))), "- [ ] <!-- task:" + taskA + " -->\n"},
		{"unknown nodes", doc(`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[`+p(txt("c1"))+`]},{"type":"tableCell","content":[`+p(txt("c2"))+`]}]}]}`,
			p(txt("a"), `{"type":"mention","attrs":{"id":"x"},"content":[{"type":"text","text":"@bob"}]}`)),
			"c1 c2\n\na@bob\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toMD(t, tc.doc); got != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestToMarkdownErrorsAndNull(t *testing.T) {
	if _, err := pmmd.ToMarkdown(json.RawMessage(`{"type":`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	for _, in := range []string{"", "null", "  "} {
		md, err := pmmd.ToMarkdown(json.RawMessage(in))
		if err != nil || md != "" {
			t.Fatalf("ToMarkdown(%q) = %q, %v", in, md, err)
		}
	}
}

// ---- round trips ----

// Markdown in canonical form must survive md -> pm -> md unchanged.
func TestMarkdownRoundTripStable(t *testing.T) {
	cases := []string{
		"# Title\n\nSome **bold**, *italic*, ~~strike~~ and `code`.\n",
		"## Links\n\n[**bold link**](https://ex.com) and [plain](/rel/path) and [mail](mailto:a@b.co)\n",
		"- a\n  - b\n    - c\n      - d\n- e\n",
		"1. one\n2. two\n   1. nested\n   2. nested\n3. three\n",
		"- [ ] todo <!-- task:" + taskA + " -->\n- [x] done <!-- task:" + taskB + " -->\n  - child\n",
		"> quote\n>\n> > nested\n>\n> - item\n",
		"```markdown\n# heading\n- list\n**bold** <!-- task:" + taskA + " -->\n```\n",
		"line one\\\nline two\n",
		"![alt](https://ex.com/i.png \"t\")\n",
		"a\n\n---\n\nb\n",
		"\\# not heading\n\n\\- not list\n\n1\\. not ordered\n\n\\> not quote\n",
		"snake_case and 2 \\* 3 and \\[brackets\\] and a\\\\b\n",
		"- a\n\n  second paragraph\n\n- b\n",
		"- item\n\n  ```go\n  fmt.Println(\"x\")\n\n  // blank above\n  ```\n",
	}
	for _, md := range cases {
		t.Run(md, func(t *testing.T) {
			pm := fromMD(t, md)
			out, err := pmmd.ToMarkdown(pm)
			if err != nil {
				t.Fatal(err)
			}
			if out != md {
				t.Fatalf("round trip changed markdown\nin:  %q\nout: %q", md, out)
			}
		})
	}
}

// Arbitrary (non-canonical) markdown must reach a fixed point after one pass.
func TestMarkdownRoundTripFixedPoint(t *testing.T) {
	cases := []string{
		"Title\n# H #\n* a\n* b\n    * c\n+ d\n\n2) x\n3) y",
		"__bold__ _it_ ***both*** **a *b* c**",
		"text  \nhard\n\n> q\ncontinued?",
		"- [X] a\n-   [ ] b\n\n\n\n- c",
		"<https://ex.com> <a@b.co> [x](<https://ex.com/a b>)",
		"~~~\ncode\n~~~",
		"***\n___\n- - -",
		"| a | b |\n|---|---|\n| 1 | 2 |",
		"1. a\n\n   b\n2. c",
		"**unclosed *emphasis",
		"x_y_z *a*b* [not](a link",
	}
	for _, md := range cases {
		t.Run(md, func(t *testing.T) {
			pm1 := fromMD(t, md)
			md1, err := pmmd.ToMarkdown(pm1)
			if err != nil {
				t.Fatal(err)
			}
			pm2 := fromMD(t, md1)
			assertSameDoc(t, pm1, pm2)
			md2, _ := pmmd.ToMarkdown(pm2)
			if md1 != md2 {
				t.Fatalf("not a fixed point\nmd1: %q\nmd2: %q", md1, md2)
			}
		})
	}
}

// ProseMirror docs must survive pm -> md -> pm (ignoring nodeIds).
func TestDocRoundTrip(t *testing.T) {
	b, i, s, c := mk("bold"), mk("italic"), mk("strike"), mk("code")
	cases := []struct{ name, doc string }{
		{"paragraphs", doc(p(txt("hello")), p(txt("world")))},
		{"all headings", doc(h(1, txt("1")), h(2, txt("2")), h(3, txt("3")), h(4, txt("4")), h(5, txt("5")), h(6, txt("6")))},
		{"marks", doc(p(txt("a", b), txt(" "), txt("b", i), txt(" "), txt("c", s), txt(" "), txt("d", c)))},
		{"overlapping marks", doc(p(txt("a", b), txt("b", b, i), txt("c", i)))},
		{"mark spans", doc(p(txt("x "), txt("bold ", b), txt("both", b, i), txt("it", i), txt(" y")))},
		{"strike and bold", doc(p(txt("s", s), txt("sb", s, b), txt("b", b)))},
		{"code inside bold", doc(p(txt("see ", b), txt("f()", b, c), txt(" now", b)))},
		{"link with bold text", doc(p(txt("go "), txt("here", b, link("https://ex.com/p?q=1&r=2")), txt(" now", link("https://ex.com/p?q=1&r=2"))))},
		{"adjacent links", doc(p(txt("a", link("https://a.com")), txt("b", link("https://b.com"))))},
		{"link with parens", doc(p(txt("wiki", link("https://en.wikipedia.org/wiki/Foo_(bar)"))))},
		{"code with backticks", doc(p(txt("a ``b`` c", c)))},
		{"hard breaks", doc(p(txt("a"), mk("hardBreak"), txt("b", b), mk("hardBreak"), txt("c")))},
		{"bold across hard break", doc(p(txt("a", b), mk("hardBreak"), txt("b", b)))},
		{"blockquote", doc(quote(p(txt("q")), quote(p(txt("nested"))), ul(li(p(txt("in quote"))))))},
		{"code blocks", doc(code("go", "func main() {\n\tfmt.Println(\"**hi**\")\n}"), code("md", "# h\n- l\n> q\n```inner```\n~~~"))},
		{"hr and image", doc(p(txt("a")), mk("horizontalRule"), `{"type":"image","attrs":{"src":"https://ex.com/x.png","alt":"x","title":"T"}}`)},
		{"nested lists 3 deep", doc(ul(
			li(p(txt("l1")), ul(li(p(txt("l2")), ol(li(p(txt("l3a"))), li(p(txt("l3b"))))))),
			li(p(txt("l1b"))),
		))},
		{"list item with multiple paragraphs", doc(ul(li(p(txt("a")), p(txt("b"))), li(p(txt("c")))))},
		{"list item with code and quote", doc(ul(li(p(txt("a")), code("sh", "ls\n\n-la")), li(p(txt("b")), quote(p(txt("q"))))))},
		{"ordered list numbering", doc(ol(li(p(txt("1"))), li(p(txt("2"))), li(p(txt("3"))), li(p(txt("4"))), li(p(txt("5"))), li(p(txt("6"))), li(p(txt("7"))), li(p(txt("8"))), li(p(txt("9"))), li(p(txt("10")), ul(li(p(txt("nested under 10"))))), li(p(txt("11")))))},
		{"adjacent lists", doc(ul(li(p(txt("a")))), ul(li(p(txt("b")))), ul(li(p(txt("c")))), ol(li(p(txt("d")))), ol(li(p(txt("e")))))},
		{"tasks", doc(h(2, txt("TODO")), ul(
			task(taskA, "todo", false, p(txt("write tests"))),
			task(taskB, "done", true, p(txt("ship")), ul(li(p(txt("sub"))))),
		))},
		{"empty list item", doc(ul(li(p()), li(p(txt("x")))))},
		{"escaping", doc(
			p(txt("# not a heading")),
			p(txt("- not a list")),
			p(txt("+ not a list")),
			p(txt("1. not ordered")),
			p(txt("2) not ordered")),
			p(txt("> not a quote")),
			p(txt("--- not a rule")),
			p(txt("---")),
			p(txt("***")),
			p(txt("```")),
			p(txt("~~~")),
			p(txt("*star* _under_ __dunder__ **bold**")),
			p(txt("`tick` [link](x) ![img](y) <b>html</b> <!-- c -->")),
			p(txt(`back\slash \* ~~strike~~ ~single~ ~`)),
			p(txt("snake_case _leading trailing_ a_ _b")),
			p(txt("[ ] fake checkbox")),
			p(txt("<https://not.linked>")),
			p(txt("a"), mk("hardBreak"), txt("# after break")),
			p(txt("100. big")),
			p(txt("[x] <!-- task:"+taskA+" -->")),
		)},
		{"escaping in list items", doc(ul(li(p(txt("[ ] not a task"))), li(p(txt("- dash"))), li(p(txt("<!-- task:"+taskA+" -->")))))},
		{"heading marks", doc(h(2, txt("Big "), txt("bold", b), txt(" "), txt("code", c)))},
		{"unicode", doc(p(txt("héllo wörld — ✓ 日本語 "), txt("強調", b), txt(" "), txt("_x_", i), txt(" "), txt("*x*", b, i)))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertValid(t, []byte(tc.doc))
			md := toMD(t, tc.doc)
			pm := fromMD(t, md)
			w := normalize(decode(t, []byte(tc.doc)))
			g := normalize(decode(t, pm))
			if !reflect.DeepEqual(w, g) {
				t.Fatalf("round trip mismatch\nmarkdown:\n%s\nwant: %s\ngot:  %s", md, mustJSON(t, w), mustJSON(t, g))
			}
		})
	}
}

// ---- tasks ----

func TestTaskMarkerRoundTrip(t *testing.T) {
	md := "## TODO\n\n- [ ] first <!-- task:" + taskA + " -->\n- [x] second <!--task:" + strings.ToUpper(taskB) + "-->\n- [ ] bad id <!-- task:not-a-uuid -->\n- no box <!-- task:" + taskC + " -->\n"
	pm := fromMD(t, md)
	var d struct {
		Content []struct {
			Type    string `json:"type"`
			Content []struct {
				Attrs   map[string]any `json:"attrs"`
				Content []struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"content"`
			} `json:"content"`
		} `json:"content"`
	}
	if err := json.Unmarshal(pm, &d); err != nil {
		t.Fatal(err)
	}
	items := d.Content[1].Content
	if len(items) != 4 {
		t.Fatalf("want 4 items, got %d", len(items))
	}
	check := func(idx int, taskID any, checked bool, status any, text string) {
		t.Helper()
		a := items[idx].Attrs
		if a["taskId"] != taskID || a["checked"] != checked || a["taskStatus"] != status {
			t.Fatalf("item %d attrs = %v", idx, a)
		}
		if got := items[idx].Content[0].Content[0].Text; got != text {
			t.Fatalf("item %d text = %q, want %q (comment must be stripped)", idx, got, text)
		}
	}
	check(0, taskA, false, "todo", "first")
	check(1, taskB, true, "done", "second") // canonicalised to lower case
	check(2, nil, false, nil, "bad id")
	check(3, taskC, false, "todo", "no box")

	out, err := pmmd.ToMarkdown(pm)
	if err != nil {
		t.Fatal(err)
	}
	want := "## TODO\n\n- [ ] first <!-- task:" + taskA + " -->\n- [x] second <!-- task:" + taskB + " -->\n- bad id\n- [ ] no box <!-- task:" + taskC + " -->\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}

func TestTaskCommentOnlyAppliesToOwnItem(t *testing.T) {
	pm := fromMD(t, "- parent\n  - child <!-- task:"+taskA+" -->\n")
	want := doc(ul(`{"type":"listItem","content":[` + p(txt("parent")) + `,` +
		ul(`{"type":"listItem","attrs":{"taskId":"`+taskA+`","taskStatus":"todo"},"content":[`+p(txt("child"))+`]}`) + `]}`))
	assertSameDoc(t, []byte(want), pm)
}

func TestPreserveTaskLinks(t *testing.T) {
	prev := `{"type":"doc","content":[{"type":"bulletList","content":[
		{"type":"listItem","attrs":{"nodeId":"node-a","taskId":"` + taskA + `","taskStatus":"in-progress","checked":false},"content":[` + p(txt("a")) + `]},
		{"type":"listItem","attrs":{"nodeId":"node-b","taskId":"` + taskB + `","taskStatus":"cancelled","checked":true},"content":[` + p(txt("b")) + `]},
		{"type":"listItem","attrs":{"nodeId":"node-plain"},"content":[` + p(txt("plain")) + `]}
	]}]}`
	// Agent edited the markdown: reordered, renamed, and duplicated task A.
	md := "- [x] b renamed <!-- task:" + taskB + " -->\n- plain\n- [ ] a <!-- task:" + taskA + " -->\n  - [ ] a again <!-- task:" + taskA + " -->\n- [ ] new <!-- task:" + taskC + " -->\n"
	next := fromMD(t, md)

	got, err := pmmd.PreserveTaskLinks(json.RawMessage(prev), next)
	if err != nil {
		t.Fatal(err)
	}
	assertValid(t, got)

	byTask := map[string][]map[string]any{}
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["type"] == "listItem" {
				a := n["attrs"].(map[string]any)
				if id, _ := a["taskId"].(string); id != "" {
					byTask[id] = append(byTask[id], a)
				}
			}
			if c, ok := n["content"].([]any); ok {
				for _, x := range c {
					walk(x)
				}
			}
		}
	}
	walk(decode(t, got))

	if a := byTask[taskB][0]; a["nodeId"] != "node-b" || a["taskStatus"] != "cancelled" {
		t.Fatalf("task B not preserved: %v", a)
	}
	if a := byTask[taskA][0]; a["nodeId"] != "node-a" || a["taskStatus"] != "in-progress" {
		t.Fatalf("task A not preserved: %v", a)
	}
	if a := byTask[taskA][1]; a["nodeId"] == "node-a" {
		t.Fatalf("duplicate task A item must keep its fresh nodeId: %v", a)
	}
	if a := byTask[taskC][0]; a["taskStatus"] != "todo" {
		t.Fatalf("new task C should be untouched: %v", a)
	}
	if id, _ := byTask[taskC][0]["nodeId"].(string); id == "" {
		t.Fatalf("new task C should keep generated nodeId")
	}

	// Null/empty prev is a no-op.
	same, err := pmmd.PreserveTaskLinks(nil, next)
	if err != nil || string(same) != string(next) {
		t.Fatalf("nil prev should return next unchanged")
	}
	if _, err := pmmd.PreserveTaskLinks(json.RawMessage(`{`), next); err == nil {
		t.Fatal("expected error for invalid prev")
	}
}

// TestPreserveTaskLinksChecked verifies that restoring taskStatus also keeps
// `checked` consistent: unchecking a done/cancelled linked bullet in markdown
// must not leave a self-contradictory node (checked:false + taskStatus:"done"),
// which render.go would draw as [x].
func TestPreserveTaskLinksChecked(t *testing.T) {
	prev := `{"type":"doc","content":[{"type":"bulletList","content":[
		{"type":"listItem","attrs":{"nodeId":"node-a","taskId":"` + taskA + `","taskStatus":"done","checked":true},"content":[` + p(txt("a")) + `]}
	]}]}`
	// Agent reverts the checkbox to unchecked in markdown.
	next := fromMD(t, "- [ ] a <!-- task:"+taskA+" -->\n")

	got, err := pmmd.PreserveTaskLinks(json.RawMessage(prev), next)
	if err != nil {
		t.Fatal(err)
	}
	assertValid(t, got)

	var found map[string]any
	var walk func(v any)
	walk = func(v any) {
		if n, ok := v.(map[string]any); ok {
			if n["type"] == "listItem" {
				if a, _ := n["attrs"].(map[string]any); a != nil {
					if id, _ := a["taskId"].(string); id == taskA {
						found = a
					}
				}
			}
			if c, ok := n["content"].([]any); ok {
				for _, x := range c {
					walk(x)
				}
			}
		}
	}
	walk(decode(t, got))

	if found == nil {
		t.Fatal("task A item not found")
	}
	if found["taskStatus"] != "done" {
		t.Fatalf("taskStatus should be restored to done: %v", found["taskStatus"])
	}
	if found["checked"] != true {
		t.Fatalf("checked must follow the restored status (true for done): %v", found["checked"])
	}
}

// ---- AppendMarkdown ----

func TestAppendMarkdown(t *testing.T) {
	existing := `{"type":"doc","content":[` +
		`{"type":"paragraph","attrs":{"textAlign":"center"},"content":[{"type":"text","marks":[{"type":"highlight","attrs":{"color":"#ff0"}}],"text":"keep me"}]},` +
		`{"type":"bulletList","content":[{"type":"listItem","attrs":{"nodeId":"n1","taskId":"` + taskA + `","taskStatus":"in-progress","checked":false},"content":[` + p(txt("task")) + `]}]}` +
		`]}`
	got, err := pmmd.AppendMarkdown(json.RawMessage(existing), "## Added\n\n- new item")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Type    string            `json:"type"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(got, &d); err != nil {
		t.Fatal(err)
	}
	var orig struct {
		Content []json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal([]byte(existing), &orig)
	if d.Type != "doc" || len(d.Content) != 4 {
		t.Fatalf("unexpected doc: %s", got)
	}
	for i := range orig.Content {
		if !reflect.DeepEqual(decode(t, orig.Content[i]), decode(t, d.Content[i])) {
			t.Fatalf("existing node %d changed:\n%s\n%s", i, orig.Content[i], d.Content[i])
		}
	}
	assertSameDoc(t, []byte(h(2, txt("Added"))), d.Content[2])
	assertSameDoc(t, []byte(ul(li(p(txt("new item"))))), d.Content[3])

	cases := []struct{ name, existing, md, want string }{
		{"null existing", "null", "hello", doc(p(txt("hello")))},
		{"empty existing", "", "# h", doc(h(1, txt("h")))},
		{"placeholder replaced", doc(p()), "text", doc(p(txt("text")))},
		{"empty md keeps doc", doc(p(txt("x"))), "", doc(p(txt("x")))},
		{"empty everything", "", "", doc(p())},
		{"no content key", `{"type":"doc"}`, "a", doc(p(txt("a")))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pmmd.AppendMarkdown(json.RawMessage(tc.existing), tc.md)
			if err != nil {
				t.Fatal(err)
			}
			assertValid(t, got)
			assertSameDoc(t, []byte(tc.want), got)
		})
	}

	if _, err := pmmd.AppendMarkdown(json.RawMessage(`{"type":"paragraph"}`), "x"); err == nil {
		t.Fatal("expected error for non-doc existing")
	}
	if _, err := pmmd.AppendMarkdown(json.RawMessage(`[`), "x"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---- AppendTaskBullet ----

func TestAppendTaskBullet(t *testing.T) {
	const nid = "11111111-1111-4111-8111-111111111111"
	newItem := func(text, status string) string {
		return `{"type":"listItem","attrs":{"nodeId":"` + nid + `","taskId":"` + taskC + `","taskStatus":"` + status + `","checked":` +
			map[bool]string{true: "true", false: "false"}[status == "done"] + `},"content":[` + p(txt(text)) + `]}`
	}
	orderedItem := li(p(txt("o")))
	existingItem := `{"type":"listItem","attrs":{"nodeId":"old","taskId":"` + taskA + `","taskStatus":"todo","checked":false},"content":[` + p(txt("old")) + `]}`

	cases := []struct {
		name, existing, heading, status, want string
	}{
		{"heading with list appends to list",
			doc(p(txt("intro")), h(2, txt("TODO")), ul(existingItem), p(txt("after"))), "", "todo",
			doc(p(txt("intro")), h(2, txt("TODO")), ul(existingItem, newItem("new", "todo")), p(txt("after")))},
		{"heading without list inserts list",
			doc(h(1, txt("TODO")), p(txt("notes"))), "", "todo",
			doc(h(1, txt("TODO")), ul(newItem("new", "todo")), p(txt("notes")))},
		{"heading at end",
			doc(p(txt("x")), h(3, txt("TODO"))), "", "done",
			doc(p(txt("x")), h(3, txt("TODO")), ul(newItem("new", "done")))},
		{"list after heading is ordered: insert new bullet list",
			doc(h(2, txt("TODO")), ol(orderedItem)), "", "todo",
			doc(h(2, txt("TODO")), ul(newItem("new", "todo")), ol(orderedItem))},
		{"no heading appends heading and list",
			doc(p(txt("x"))), "", "todo",
			doc(p(txt("x")), h(2, txt("TODO")), ul(newItem("new", "todo")))},
		{"case-insensitive match",
			doc(h(2, txt("To"), txt("do", mk("bold"))), ul(existingItem)), "todo", "todo",
			doc(h(2, txt("To"), txt("do", mk("bold"))), ul(existingItem, newItem("new", "todo")))},
		{"custom heading",
			doc(h(2, txt("TODO")), h(2, txt("action items")), ul(existingItem)), "Action Items", "todo",
			doc(h(2, txt("TODO")), h(2, txt("action items")), ul(existingItem, newItem("new", "todo")))},
		{"custom heading missing is created with that text",
			doc(p(txt("x"))), "Next Steps", "todo",
			doc(p(txt("x")), h(2, txt("Next Steps")), ul(newItem("new", "todo")))},
		{"first matching heading wins",
			doc(h(2, txt("TODO")), p(txt("gap")), h(2, txt("todo")), ul(existingItem)), "", "todo",
			doc(h(2, txt("TODO")), ul(newItem("new", "todo")), p(txt("gap")), h(2, txt("todo")), ul(existingItem))},
		{"empty doc", "", "", "todo",
			doc(h(2, txt("TODO")), ul(newItem("new", "todo")))},
		{"placeholder doc", doc(p()), "", "todo",
			doc(h(2, txt("TODO")), ul(newItem("new", "todo")))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pmmd.AppendTaskBullet(json.RawMessage(tc.existing), "new", nid, taskC, tc.status, tc.heading)
			if err != nil {
				t.Fatal(err)
			}
			assertValid(t, got)
			// Compare including nodeIds (all fixed here).
			if w, g := decode(t, []byte(tc.want)), decode(t, got); !reflect.DeepEqual(w, g) {
				t.Fatalf("want %s\ngot  %s", mustJSON(t, w), mustJSON(t, g))
			}
		})
	}

	t.Run("generated nodeId and markdown rendering", func(t *testing.T) {
		got, err := pmmd.AppendTaskBullet(nil, "call *Bob*", "", taskA, "todo", "")
		if err != nil {
			t.Fatal(err)
		}
		md, err := pmmd.ToMarkdown(got)
		if err != nil {
			t.Fatal(err)
		}
		if want := "## TODO\n\n- [ ] call \\*Bob\\* <!-- task:" + taskA + " -->\n"; md != want {
			t.Fatalf("got %q want %q", md, want)
		}
		if !strings.Contains(string(got), `"nodeId":"`) || strings.Contains(string(got), `"nodeId":""`) {
			t.Fatalf("expected generated nodeId: %s", got)
		}
	})
}

// FuzzMarkdown checks that arbitrary input never panics, always yields a doc
// the server validator accepts unchanged, and renders back to Markdown that
// also parses to a valid doc.
func FuzzMarkdown(f *testing.F) {
	for _, s := range []string{
		"", "# h", "- [ ] a <!-- task:" + taskA + " -->", "**a *b** c*", "[a](<b)", "> > - ```\n x",
		"1. a\n   - b\n\n     c", "`` ` ``", "\\", "![a](b \"c", "***a**b*", "~~~~a~~", "<!--", "\t- a\n\t\t- b",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, md string) {
		pm := fromMD(t, md)
		out, err := pmmd.ToMarkdown(pm)
		if err != nil {
			t.Fatal(err)
		}
		fromMD(t, out)
	})
}
