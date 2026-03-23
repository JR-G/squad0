package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineOutcome_ExitCodeZero_ReturnsSuccess(t *testing.T) {
	t.Parallel()

	result := agent.SessionResult{ExitCode: 0}
	assert.Equal(t, "success", string(agent.DetermineOutcome(result)))
}

func TestDetermineOutcome_NonZeroExitCode_ReturnsFailure(t *testing.T) {
	t.Parallel()

	result := agent.SessionResult{ExitCode: 1}
	assert.Equal(t, "failure", string(agent.DetermineOutcome(result)))
}

func TestDetermineOutcome_ErrorMessageInOutput_ReturnsPartial(t *testing.T) {
	t.Parallel()

	errorContent, _ := json.Marshal("context limit reached")
	result := agent.SessionResult{
		ExitCode: 0,
		Messages: []agent.StreamMessage{
			{Type: "error", Content: errorContent},
		},
	}
	assert.Equal(t, "partial", string(agent.DetermineOutcome(result)))
}

func TestDetermineOutcome_EmptyErrorContent_ReturnsSuccess(t *testing.T) {
	t.Parallel()

	errorContent, _ := json.Marshal("")
	result := agent.SessionResult{
		ExitCode: 0,
		Messages: []agent.StreamMessage{
			{Type: "error", Content: errorContent},
		},
	}
	assert.Equal(t, "success", string(agent.DetermineOutcome(result)))
}

func TestDetermineOutcome_InvalidErrorJSON_ReturnsSuccess(t *testing.T) {
	t.Parallel()

	result := agent.SessionResult{
		ExitCode: 0,
		Messages: []agent.StreamMessage{
			{Type: "error", Content: json.RawMessage(`not valid json`)},
		},
	}
	assert.Equal(t, "success", string(agent.DetermineOutcome(result)))
}

func TestTruncateSummary_ShortText_ReturnsUnchanged(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "short", agent.TruncateSummary("short", 500))
}

func TestTruncateSummary_LongText_Truncates(t *testing.T) {
	t.Parallel()

	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}

	result := agent.TruncateSummary(string(long), 500)

	assert.Len(t, result, 500)
}

func TestTruncateSummary_ExactLength_ReturnsUnchanged(t *testing.T) {
	t.Parallel()

	exact := make([]byte, 500)
	for i := range exact {
		exact[i] = 'x'
	}

	result := agent.TruncateSummary(string(exact), 500)

	assert.Len(t, result, 500)
}

func TestExecProcessRunner_Run_InvalidCommand_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := agent.ExecProcessRunner{}
	_, err := runner.Run(context.Background(), "", "", "nonexistent-binary-that-does-not-exist")

	require.Error(t, err)
}

func TestExecProcessRunner_Run_WithWorkingDir_RunsInDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	runner := agent.ExecProcessRunner{}
	output, err := runner.Run(context.Background(), "", tmpDir, "pwd")

	require.NoError(t, err)
	assert.Contains(t, string(output), tmpDir)
}

func TestExtractExitError_NonExitError_ReturnsOne(t *testing.T) {
	t.Parallel()

	code := agent.ExtractExitError(fmt.Errorf("random error"))

	assert.Equal(t, 1, code)
}
