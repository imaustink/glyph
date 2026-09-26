package handler

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// editorSchema is testdata/editor_schema.json: the node and mark types of the
// editor's documentSchema() (src/lib/editor/schema.ts) with their attribute
// names. src/lib/editor/schema.fixture.test.ts keeps the fixture equal to the
// live schema; regenerate it with `pnpm schema:fixture`.
type editorSchema struct {
	Nodes map[string][]string `json:"nodes"`
	Marks map[string][]string `json:"marks"`
}

func loadEditorSchema(t *testing.T) editorSchema {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_schema.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var s editorSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return s
}

// allowlistAsSchema renders the validator's allowlist in the fixture's shape.
func allowlistAsSchema(types map[string]bool, attrs map[string]map[string]bool) map[string][]string {
	out := map[string][]string{}
	for name, ok := range types {
		if !ok {
			continue
		}
		keys := []string{}
		for k, allowed := range attrs[name] {
			if allowed {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		out[name] = keys
	}
	return out
}

// TestAllowlistMatchesEditorSchema fails when the validator allowlist and the
// editor schema diverge. Accepting a node or mark the editor lacks makes the
// note load blank (and the next keystroke saves the blank doc over it —
// DI-01); stripping an attribute the editor has loses it on every save
// (DI-24).
func TestAllowlistMatchesEditorSchema(t *testing.T) {
	want := loadEditorSchema(t)

	if got := allowlistAsSchema(allowedNodeTypes, allowedAttrs); !reflect.DeepEqual(got, want.Nodes) {
		t.Errorf("node allowlist differs from the editor schema\nallowlist: %v\neditor:    %v", got, want.Nodes)
	}
	for name := range allowedAttrs {
		if !allowedNodeTypes[name] {
			t.Errorf("allowedAttrs has an entry for %q, which is not an allowed node type", name)
		}
	}
	if got := allowlistAsSchema(allowedMarkTypes, allowedMarkAttrs); !reflect.DeepEqual(got, want.Marks) {
		t.Errorf("mark allowlist differs from the editor schema\nallowlist: %v\neditor:    %v", got, want.Marks)
	}
	for name := range allowedMarkAttrs {
		if !allowedMarkTypes[name] {
			t.Errorf("allowedMarkAttrs has an entry for %q, which is not an allowed mark type", name)
		}
	}
}

// TestValidatorKeepsEditorAttrs: orderedList start/type and link title are
// editor attributes and must survive a save (DI-24).
func TestValidatorKeepsEditorAttrs(t *testing.T) {
	in := `{"type":"doc","content":[` +
		`{"type":"orderedList","attrs":{"start":3,"type":"a"},"content":[{"type":"listItem","attrs":{"nodeId":"n1"},"content":[{"type":"paragraph","content":[` +
		`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://ex.com","title":"Example"}}]}]}]}]}]}`
	out, err := ValidateProseMirrorContent([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	_ = json.Unmarshal([]byte(in), &want)
	_ = json.Unmarshal(out, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("validator altered editor attributes\nin:  %s\nout: %s", in, out)
	}
}

// TestValidatorDowngradesNodesTheEditorLacks: stored or submitted content
// with a node the editor can't represent must not be dropped silently (it
// used to be kept, which blanked the editor — DI-01). It's downgraded to
// something the editor has, keeping its text and, for images, the URL.
func TestValidatorDowngradesNodesTheEditorLacks(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "block image becomes a link paragraph",
			in:   `{"type":"doc","content":[{"type":"image","attrs":{"src":"https://ex.com/c.png","alt":"A cat","title":"T"}}]}`,
			want: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"A cat","marks":[{"type":"link","attrs":{"href":"https://ex.com/c.png"}}]}]}]}`,
		},
		{
			name: "image without alt uses its URL as the text",
			in:   `{"type":"doc","content":[{"type":"image","attrs":{"src":"https://ex.com/c.png"}}]}`,
			want: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"https://ex.com/c.png","marks":[{"type":"link","attrs":{"href":"https://ex.com/c.png"}}]}]}]}`,
		},
		{
			name: "image with an unsafe URL keeps only its alt text",
			in:   `{"type":"doc","content":[{"type":"image","attrs":{"src":"javascript:alert(1)","alt":"x"}}]}`,
			want: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`,
		},
		{
			name: "inline unknown node becomes its text",
			in:   `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a "},{"type":"mention","attrs":{"id":"u1"},"content":[{"type":"text","text":"@bob"}]}]}]}`,
			want: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a "},{"type":"text","text":"@bob"}]}]}`,
		},
		{
			name: "unknown container keeps its known children",
			in:   `{"type":"doc","content":[{"type":"details","content":[{"type":"paragraph","content":[{"type":"text","text":"inside"}]}]}]}`,
			want: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"inside"}]}]}`,
		},
		{
			name: "unknown node inside a list becomes a list item",
			in:   `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"taskItem","content":[{"type":"paragraph","content":[{"type":"text","text":"t"}]}]}]}]}`,
			want: `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"t"}]}]}]}]}`,
		},
		{
			name: "highlight, subscript and superscript marks are dropped, text kept",
			in:   `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"h","marks":[{"type":"highlight","attrs":{"color":"red"}},{"type":"bold"}]},{"type":"text","text":"2","marks":[{"type":"subscript"}]},{"type":"text","text":"n","marks":[{"type":"superscript"}]}]}]}`,
			want: `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"h","marks":[{"type":"bold"}]},{"type":"text","text":"2"},{"type":"text","text":"n"}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ValidateProseMirrorContent([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal(out, &got)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("\nwant: %s\ngot:  %s", tc.want, out)
			}
		})
	}
}
