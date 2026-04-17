package logging_test

import (
	"bytes"
	"testing"

	"github.com/JR-G/squad0/internal/logging"
	"github.com/stretchr/testify/assert"
)

func TestConsoleWriter_FormatsOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("2026/03/27 15:07:33 tick: 5 idle agents"))
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "tick")
	assert.Contains(t, output, "idle agents")
}

func TestConsoleWriter_EmptyInput_NoOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("   "))
	assert.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestConsoleWriter_SuppressesNoiseLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"work_enabled", "tick: work_enabled=true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			writer := logging.NewConsoleWriter(&buf)
			_, _ = writer.Write([]byte(tt.input))
			assert.Empty(t, buf.String(), "should suppress: %s", tt.input)
		})
	}
}

func TestConsoleWriter_AllCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		category string
	}{
		{"tick: 3 idle agents", "tick"},
		{"resume: JAM-7 approved", "resume"},
		{"resuming JAM-7", "resume"},
		{"review: starting review", "review"},
		{"re-review: completed", "review"},
		{"fix-up: engineer starting", "fixup"},
		{"merge: JAM-7 has conflicts", "merge"},
		{"idle duty: tech-lead reviewed", "idle"},
		{"own pr check: engineer-1 followed up", "own-pr"},
		{"orchestrator started", "system"},
		{"socket event: connected", "socket"},
		{"chat: engineer-2 responding", "chat"},
		{"message received: channel=commands", "slack"},
		{"work item JAM-26 is stale", "pipeline"},
		{"engineer merge failed", "merge"},
		{"session error for engineer-1", "error"},
		{"rescue pr failed for JAM-1", "rescue"},
		{"PM said: assignments", "assign"},
		{"rebase session failed", "rebase"},
		{"something random happened", "info"},
		{"operation failed completely", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			writer := logging.NewConsoleWriter(&buf)
			_, err := writer.Write([]byte(tt.input))
			assert.NoError(t, err)
			assert.Contains(t, buf.String(), tt.category)
		})
	}
}

func TestConsoleWriter_SetRoster_ReplacesRoles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)
	writer.SetRoster(map[string]string{
		"engineer-1": "Callum",
		"engineer-2": "Mara",
		"tech-lead":  "Sable",
	})

	_, _ = writer.Write([]byte("idle duty: tech-lead reviewed engineer-2's PR"))

	output := buf.String()
	assert.Contains(t, output, "Sable")
	assert.Contains(t, output, "Mara")
}

// Regression: roster substitution used to mangle file paths and
// any other token that contained a role substring. Real example
// from the wild: a worktree path ".worktrees/engineer-2-fixup" was
// being logged as ".worktrees/Mara (engineer-2)-fixup", which
// looked like a real path bug but wasn't.
func TestConsoleWriter_SetRoster_DoesNotMangleFilePaths(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)
	writer.SetRoster(map[string]string{"engineer-2": "Mara"})

	_, _ = writer.Write([]byte("created worktree at /repo/.worktrees/engineer-2-fixup (branch feat)"))

	output := buf.String()
	assert.Contains(t, output, ".worktrees/engineer-2-fixup")
	assert.NotContains(t, output, "Mara (engineer-2)-fixup")
}

func TestConsoleWriter_SetRoster_StandaloneRoleReplaced(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)
	writer.SetRoster(map[string]string{"engineer-2": "Mara"})

	_, _ = writer.Write([]byte("session engineer-2 finished"))

	assert.Contains(t, buf.String(), "Mara")
}

func TestConsoleWriter_SetRoster_RoleAtEndOfLineReplaced(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)
	writer.SetRoster(map[string]string{"engineer-2": "Mara"})

	_, _ = writer.Write([]byte("respawned engineer-2"))

	assert.Contains(t, buf.String(), "Mara")
}

func TestConsoleWriter_StripLogTimestamp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, _ = writer.Write([]byte("2026/03/27 15:07:33 orchestrator started"))
	assert.Contains(t, buf.String(), "system")

	// Without timestamp prefix.
	buf.Reset()
	_, _ = writer.Write([]byte("orchestrator started"))
	assert.Contains(t, buf.String(), "system")
}

func TestConsoleWriter_HighlightsUnknownRoles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	// No roster set — should still highlight role names.
	_, _ = writer.Write([]byte("idle duty: reviewer checked PR"))
	assert.Contains(t, buf.String(), "reviewer")
}
