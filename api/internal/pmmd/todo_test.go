package pmmd_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/glyph/api/internal/pmmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bulletTexts(bs []pmmd.TodoBullet) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Text
	}
	return out
}

func TestFindUnlinkedTodoBullets(t *testing.T) {
	const linked = "1b4e28ba-2d1e-4a3b-9d9b-3c1f6f6c0e2a"
	cases := []struct {
		name    string
		md      string
		trigger pmmd.TodoTrigger
		want    []string
	}{
		{"default heading", "## TODO\n\n- a\n- b\n", pmmd.TodoTrigger{}, []string{"a", "b"}},
		{"case-insensitive", "# todo\n\n- a\n", pmmd.TodoTrigger{}, []string{"a"}},
		{"not under TODO", "- a\n\n## Notes\n\n- b\n", pmmd.TodoTrigger{}, nil},
		{"section ends at next heading", "## TODO\n\n- a\n\n## Done\n\n- b\n", pmmd.TodoTrigger{}, []string{"a"}},
		{"paragraph does not end section", "## TODO\n\nsome words\n\n- a\n", pmmd.TodoTrigger{}, []string{"a"}},
		{"nested bullets", "## TODO\n\n- a\n  - a1\n    - a2\n", pmmd.TodoTrigger{}, []string{"a", "a1", "a2"}},
		{"ordered lists ignored", "## TODO\n\n1. a\n2. b\n", pmmd.TodoTrigger{}, nil},
		{"linked skipped", "## TODO\n\n- [ ] done already <!-- task:" + linked + " -->\n- new\n", pmmd.TodoTrigger{}, []string{"new"}},
		{"custom exact", "## Action items\n\n- a\n\n## TODO\n\n- b\n", pmmd.TodoTrigger{Pattern: "action items", MatchMode: "exact"}, []string{"a"}},
		{"regex", "## Tasks for Monday\n\n- a\n", pmmd.TodoTrigger{Pattern: "^Tasks", MatchMode: "regex"}, []string{"a"}},
		{"invalid regex matches nothing", "## TODO\n\n- a\n", pmmd.TodoTrigger{Pattern: "(", MatchMode: "regex"}, nil},
		{"paragraph trigger", "TODO\n\n- a\n", pmmd.TodoTrigger{Pattern: "TODO", MatchMode: "exact", BlockTypes: []string{"paragraph"}}, []string{"a"}},
		{"heading not a trigger type", "## TODO\n\n- a\n", pmmd.TodoTrigger{Pattern: "TODO", MatchMode: "exact", BlockTypes: []string{"paragraph"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := pmmd.FromMarkdown(tc.md)
			require.NoError(t, err)
			_, got, err := pmmd.FindUnlinkedTodoBullets(doc, tc.trigger)
			require.NoError(t, err)
			if tc.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tc.want, bulletTexts(got))
			}
			for _, b := range got {
				assert.NotEmpty(t, b.NodeID)
			}
		})
	}
}

// todoFixture is testdata/todo_derivation.json, which the editor's test
// (src/lib/editor/todoDerivation.test.ts) reads too: both sides must derive
// the same tasks from the same content (DI-27).
type todoFixture struct {
	Detection []struct {
		Name    string `json:"name"`
		Trigger *struct {
			Pattern    string   `json:"pattern"`
			MatchMode  string   `json:"matchMode"`
			BlockTypes []string `json:"blockTypes"`
		} `json:"trigger"`
		Doc  json.RawMessage `json:"doc"`
		Want []string        `json:"want"`
	} `json:"detection"`
	Checked []struct {
		Status  string `json:"status"`
		Checked bool   `json:"checked"`
	} `json:"checked"`
}

func loadTodoFixture(t *testing.T) todoFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/todo_derivation.json")
	require.NoError(t, err)
	var f todoFixture
	require.NoError(t, json.Unmarshal(raw, &f))
	require.NotEmpty(t, f.Detection)
	return f
}

func TestTodoDerivationSharedFixture(t *testing.T) {
	f := loadTodoFixture(t)
	for _, tc := range f.Detection {
		t.Run(tc.Name, func(t *testing.T) {
			trigger := pmmd.TodoTrigger{}
			if tc.Trigger != nil {
				trigger = pmmd.TodoTrigger{Pattern: tc.Trigger.Pattern, MatchMode: tc.Trigger.MatchMode, BlockTypes: tc.Trigger.BlockTypes}
			}
			_, got, err := pmmd.FindUnlinkedTodoBullets(tc.Doc, trigger)
			require.NoError(t, err)
			texts := bulletTexts(got)
			if len(tc.Want) == 0 {
				assert.Empty(t, texts)
			} else {
				assert.Equal(t, tc.Want, texts)
			}
		})
	}
}

func TestTodoDerivationSharedFixture_Checked(t *testing.T) {
	f := loadTodoFixture(t)
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"bulletList","content":[
		{"type":"listItem","attrs":{"nodeId":"n"},"content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}]}]}`)
	for _, tc := range f.Checked {
		t.Run(tc.Status, func(t *testing.T) {
			out, err := pmmd.LinkTodoBullets(doc, map[string]pmmd.TaskLink{"n": {TaskID: "t", Status: tc.Status}})
			require.NoError(t, err)
			var root struct {
				Content []struct {
					Content []struct {
						Attrs map[string]interface{} `json:"attrs"`
					} `json:"content"`
				} `json:"content"`
			}
			require.NoError(t, json.Unmarshal(out, &root))
			assert.Equal(t, tc.Checked, root.Content[0].Content[0].Attrs["checked"])
		})
	}
}

func TestFindAssignsMissingNodeIDsAndLinks(t *testing.T) {
	doc := json.RawMessage(`{"type":"doc","content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"TODO"}]},
		{"type":"bulletList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"no id"}]}]},
			{"type":"listItem","attrs":{"nodeId":"keep-me","checked":true},"content":[{"type":"paragraph","content":[{"type":"text","text":"ticked"}]}]}
		]}]}`)
	out, bullets, err := pmmd.FindUnlinkedTodoBullets(doc, pmmd.TodoTrigger{})
	require.NoError(t, err)
	require.Len(t, bullets, 2)
	assert.NotEmpty(t, bullets[0].NodeID)
	assert.Equal(t, "keep-me", bullets[1].NodeID)
	assert.True(t, bullets[1].Checked)

	linked, err := pmmd.LinkTodoBullets(out, map[string]pmmd.TaskLink{
		bullets[0].NodeID: {TaskID: "t1", Status: "todo"},
		"keep-me":         {TaskID: "t2", Status: "done"},
	})
	require.NoError(t, err)
	_, again, err := pmmd.FindUnlinkedTodoBullets(linked, pmmd.TodoTrigger{})
	require.NoError(t, err)
	assert.Empty(t, again, "linked bullets must not be found again")

	md, err := pmmd.ToMarkdown(linked)
	require.NoError(t, err)
	assert.Contains(t, md, "- [ ] no id <!-- task:t1 -->")
	assert.Contains(t, md, "- [x] ticked <!-- task:t2 -->")
}
