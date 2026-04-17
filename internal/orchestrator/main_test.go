package orchestrator_test

import (
	"context"
	"os"
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
)

// TestMain stubs production hooks that previously delegated to a
// Claude session and therefore worked under the fakeProcessRunner.
// Now they shell out to real binaries (gh, etc.) — fine in production
// but unsuitable for unit tests. The default stub is the most common
// happy-path; individual tests override via the same Set*ForTest
// hooks when they need a different outcome.
func TestMain(m *testing.M) {
	restoreMergeVerifier := orchestrator.SetMergeVerifierForTest(func(_ context.Context, _, _ string) bool {
		return true
	})

	code := m.Run()
	restoreMergeVerifier()
	os.Exit(code)
}
