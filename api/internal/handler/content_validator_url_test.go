package handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSafeURL(t *testing.T) {
	safe := []string{
		"",
		"https://example.com/a?b=c#d",
		"http://example.com",
		"HTTPS://EXAMPLE.COM",
		"mailto:someone@example.com",
		"/relative/path",
		"relative/path",
		"#fragment",
		"?query=1",
		// A colon after a path separator is part of the path, not a scheme.
		"/foo:bar",
	}
	for _, u := range safe {
		assert.True(t, isSafeURL(u), "expected safe: %q", u)
	}

	unsafe := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"  javascript:alert(1)",
		"java\tscript:alert(1)",
		"java\nscript:alert(1)",
		"jAvAsCrIpT:alert(document.cookie)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
	}
	for _, u := range unsafe {
		assert.False(t, isSafeURL(u), "expected unsafe: %q", u)
	}
}

// TestValidateProseMirrorContentStripsUnsafeURLs guards the stored-XSS path.
// Notes are shared between users, so a javascript: link persisted by one user
// executes in another user's session when clicked.
func TestValidateProseMirrorContentStripsUnsafeURLs(t *testing.T) {
	doc := `{
      "type": "doc",
      "content": [
        {
          "type": "paragraph",
          "content": [
            {
              "type": "text",
              "text": "click me",
              "marks": [{"type": "link", "attrs": {"href": "javascript:alert(1)", "target": "_blank"}}]
            },
            {
              "type": "text",
              "text": "safe",
              "marks": [{"type": "link", "attrs": {"href": "https://example.com"}}]
            }
          ]
        },
        {"type": "image", "attrs": {"src": "javascript:alert(2)", "alt": "x"}},
        {"type": "image", "attrs": {"src": "https://example.com/i.png", "alt": "ok"}}
      ]
    }`

	out, err := ValidateProseMirrorContent([]byte(doc))
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &parsed))

	content := parsed["content"].([]interface{})
	para := content[0].(map[string]interface{})
	texts := para["content"].([]interface{})

	// Unsafe href removed, the rest of the mark survives.
	badMark := texts[0].(map[string]interface{})["marks"].([]interface{})[0].(map[string]interface{})
	if attrs, ok := badMark["attrs"].(map[string]interface{}); ok {
		assert.NotContains(t, attrs, "href", "javascript: href must be stripped")
	}

	// Safe href preserved.
	goodMark := texts[1].(map[string]interface{})["marks"].([]interface{})[0].(map[string]interface{})
	goodAttrs := goodMark["attrs"].(map[string]interface{})
	assert.Equal(t, "https://example.com", goodAttrs["href"])

	// Unsafe image src removed, safe one preserved.
	badImg := content[1].(map[string]interface{})
	if attrs, ok := badImg["attrs"].(map[string]interface{}); ok {
		assert.NotContains(t, attrs, "src", "javascript: src must be stripped")
	}
	goodImg := content[2].(map[string]interface{})
	assert.Equal(t, "https://example.com/i.png", goodImg["attrs"].(map[string]interface{})["src"])
}
