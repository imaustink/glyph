package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnippet(t *testing.T) {
	long := strings.Repeat("a ", 100) + "needle" + strings.Repeat(" b", 100)
	s := snippet(long, "NEEDLE")
	assert.Contains(t, s, "needle")
	assert.True(t, strings.HasPrefix(s, "…"))
	assert.True(t, strings.HasSuffix(s, "…"))

	assert.Equal(t, "short text", snippet("short\n\ntext", "zzz"))

	// Runes whose lower-case form is longer must not misalign indexes.
	s = snippet(strings.Repeat("İ", 200)+" target", "target")
	assert.Contains(t, s, "target")
}
