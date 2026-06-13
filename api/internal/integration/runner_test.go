package integration

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// backends is populated in init() functions (or TestMain) by each
// backend_*_test.go file via registerBackend.
var backends []Backend

func registerBackend(b Backend) {
	backends = append(backends, b)
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// RunSpecs runs a collection of spec functions against every registered backend.
// Each spec receives a fresh Harness.
func RunSpecs(t *testing.T, specs map[string]func(t *testing.T, h *Harness)) {
	t.Helper()
	if len(backends) == 0 {
		t.Fatal("no backends registered — check backend_*_test.go init() functions")
	}
	for _, b := range backends {
		b := b
		t.Run(b.Name(), func(t *testing.T) {
			t.Parallel()
			h := NewHarness(t, b)
			defer b.Teardown()
			for name, spec := range specs {
				name, spec := name, spec
				t.Run(name, func(t *testing.T) {
					spec(t, h)
				})
			}
		})
	}
}
