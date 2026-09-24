package pmmd_test

import (
	"encoding/json"
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
