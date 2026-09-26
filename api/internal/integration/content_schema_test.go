package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/glyph/api/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyDoc is content stored before the validator allowlist matched the
// editor schema: an MCP-written image node and a highlight mark. The editor
// can't build either, so it loaded such a note as a blank doc (DI-01).
const legacyDoc = `{"type":"doc","content":[` +
	`{"type":"paragraph","content":[{"type":"text","text":"marked","marks":[{"type":"highlight","attrs":{"color":"yellow"}}]}]},` +
	`{"type":"image","attrs":{"src":"https://ex.com/cat.png","alt":"A cat"}},` +
	`{"type":"bulletList","content":[{"type":"listItem","attrs":{"nodeId":"n1"},"content":[{"type":"paragraph","content":[{"type":"text","text":"keep"}]}]}]}]}`

// TestLegacyContentReadsAsEditorSchema: content already stored with nodes or
// marks the editor lacks is served downgraded to the editor schema, so the
// editor can open it (rather than showing a blank doc that the next keystroke
// saves over the note) and a save of what it read is accepted.
func TestLegacyContentReadsAsEditorSchema(t *testing.T) {
	RunSpecs(t, map[string]func(t *testing.T, h *Harness){
		"GetDowngradesUnknownNodesAndMarks": func(t *testing.T, h *Harness) {
			h.ResetDB(t)
			page := createPage(t, h, h.UserA.ID, "Legacy")
			// Written straight to the store, as it was before the allowlist fix.
			_, err := h.PageHandler.Pages.UpsertContent(context.Background(), &model.PageContent{
				PageID: page.ID, Content: json.RawMessage(legacyDoc), SchemaVersion: 1, DetachCollab: true,
			}, h.UserA.ID)
			require.NoError(t, err)

			w := h.Do(t, "GET", "/api/v1/pages/"+page.ID.String()+"/content", nil, h.UserA.ID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			got := Decode[model.PageContent](t, w)
			body := string(got.Content)
			assert.NotContains(t, body, `"image"`)
			assert.NotContains(t, body, `"highlight"`)
			assert.Contains(t, body, `"href":"https://ex.com/cat.png"`, "the image URL survives as a link")
			assert.Contains(t, body, `"A cat"`)
			assert.Contains(t, body, `"marked"`)
			assert.True(t, strings.Contains(body, `"nodeId":"n1"`), "known nodes are untouched")

			// Saving back what was read is accepted.
			code, saved := putContent(t, h, page.ID.String(), h.UserA.ID, map[string]interface{}{
				"content": got.Content, "expectedRevision": got.Revision,
			})
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, got.Revision+1, saved.Revision)
		},
	})
}
