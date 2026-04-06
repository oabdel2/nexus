package router

import (
	"os"
	"strings"
	"testing"
)

// ═══════════════════════════════════════════════════════════════════════════
// Regression: Bug 4 — go.mod declares non-existent Go version
//
// The original bug: go.mod declares "go 1.26.1" which (as of Go 1.24.x)
// does not exist. This causes build failures on standard Go toolchains.
// This test verifies the go.mod declares a valid, released Go version.
// ═══════════════════════════════════════════════════════════════════════════

func TestGoModVersion(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}
	content := string(data)

	// go 1.26.x does not exist as a released Go version
	if strings.Contains(content, "go 1.26") {
		t.Error("go.mod declares Go 1.26.x which doesn't exist as a released Go version. " +
			"As of 2025, the latest Go release is 1.24.x. " +
			"Update go.mod to use a real Go version (e.g., go 1.22 or go 1.23).")
	}

	// Also check for other obviously invalid future versions
	for _, v := range []string{"go 1.27", "go 1.28", "go 1.29", "go 1.30", "go 2."} {
		if strings.Contains(content, v) {
			t.Errorf("go.mod declares %s which is not a released Go version", v)
		}
	}

	// Verify go.mod contains a go directive at all
	if !strings.Contains(content, "\ngo ") && !strings.HasPrefix(content, "go ") {
		t.Error("go.mod is missing the 'go' version directive")
	}
}
