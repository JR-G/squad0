package agent_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCodexContent_ContentField_ReturnsContent(t *testing.T) {
	t.Parallel()

	raw := `{"type":"message","content":"hello world"}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "hello world", result)
}

func TestExtractCodexContent_MessageField_ReturnsMessage(t *testing.T) {
	t.Parallel()

	raw := `{"type":"response","message":"task complete"}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "task complete", result)
}

func TestExtractCodexContent_ResultField_ReturnsResult(t *testing.T) {
	t.Parallel()

	raw := `{"type":"output","result":"final output"}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "final output", result)
}

func TestExtractCodexContent_OutputTextArray_ReturnsText(t *testing.T) {
	t.Parallel()

	raw := `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello from codex"}]}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "hello from codex", result)
}

func TestExtractCodexContent_ResponseItemPayload_ReturnsAssistantText(t *testing.T) {
	t.Parallel()

	raw := `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"payload answer"}]}}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "payload answer", result)
}

func TestExtractCodexContent_TaskComplete_ReturnsLastAgentMessage(t *testing.T) {
	t.Parallel()

	raw := `{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"done"}}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "done", result)
}

func TestExtractCodexContent_MetaEvent_Skipped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{"turn.completed", `{"type":"turn.completed","usage":{"input_tokens":100}}`},
		{"response.completed", `{"type":"response.completed"}`},
		{"thread.started", `{"type":"thread.started","thread_id":"abc"}`},
		{"turn.started", `{"type":"turn.started"}`},
		{"turn.updated", `{"type":"turn.updated"}`},
		{"item.started", `{"type":"item.started"}`},
		{"item.updated", `{"type":"item.updated"}`},
		{"token.count", `{"type":"token.count","count":42}`},
		{"usage", `{"type":"usage","total":100}`},
		{"event", `{"type":"event","name":"test"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := tt.line + "\n" + `{"type":"message","content":"actual answer"}`
			result := agent.ParseCodexOutput(raw)
			assert.Equal(t, "actual answer", result)
		})
	}
}

func TestExtractCodexContent_EmptyJSON_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	raw := `{"type":"unknown","data":"stuff"}`
	result := agent.ParseCodexOutput(raw)
	assert.Empty(t, result, "JSON with no content fields should return empty")
}

func TestExtractCodexContent_NullTranscript_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	raw := `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"null"}]}`
	result := agent.ParseCodexOutput(raw)
	assert.Empty(t, result)
}

func TestExtractCodexContent_InvalidJSON_FallsBackToPlainText(t *testing.T) {
	t.Parallel()

	raw := "this is not json at all"
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "this is not json at all", result)
}

func TestExtractCodexContent_MixedMetaAndContent(t *testing.T) {
	t.Parallel()

	raw := `{"type":"turn.started"}
{"type":"item.started"}
{"type":"message","content":"first answer"}
{"type":"message","content":"second answer"}
{"type":"turn.completed","usage":{"tokens":50}}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "second answer", result, "should return last content line")
}

func TestExtractCodexContent_OnlyMetaEvents_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	raw := `{"type":"turn.started"}
{"type":"turn.completed"}`
	result := agent.ParseCodexOutput(raw)
	assert.Empty(t, result)
}

func TestExtractCodexContent_ContentThenPlainText_PrefersContent(t *testing.T) {
	t.Parallel()

	raw := `{"type":"message","content":"structured answer"}
not json here`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "structured answer", result)
}

func TestExtractCodexContent_PlainMetaLine_Skipped(t *testing.T) {
	t.Parallel()

	raw := "Reading additional input from stdin...\n" +
		`{"type":"message","content":"real content"}`
	result := agent.ParseCodexOutput(raw)
	assert.Equal(t, "real content", result)
}

func TestBuildCodexArgs_AutoModel_OmitsModelFlag(t *testing.T) {
	t.Parallel()

	args := agent.BuildCodexArgs("prompt", "/tmp", "auto")
	assert.NotContains(t, args, "-m")
	assert.Contains(t, args, "exec")
	assert.Contains(t, args, "--skip-git-repo-check")
	assert.Contains(t, args, `model_reasoning_effort="medium"`)
	assert.Contains(t, args, "prompt")
}

func TestSession_Run_CodexFallback_EmptyTranscriptReturnsError(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &switchingRunner{
		responses: []runResult{
			{output: []byte("rate_limit_exceeded"), err: fmt.Errorf("exit 1")},
			{output: []byte(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":""}]}`), err: nil},
		},
		callCount: &callCount,
	}

	session := agent.NewSession(runner)
	session.SetCodexFallback("auto")

	_, err := session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RoleEngineer1,
		Model:  "claude-sonnet-4-6",
		Prompt: "test",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex returned empty response")
}

func TestSession_Run_CodexFallback_UsesLastMessageFileWhenStdoutEmpty(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &codexLastMessageRunner{
		responses: []runResult{
			{output: []byte("rate_limit_exceeded"), err: fmt.Errorf("exit 1")},
			{output: []byte(`{"type":"thread.started"}` + "\n"), err: nil},
		},
		callCount:        &callCount,
		lastMessageValue: "codex answer from last-message file",
	}

	session := agent.NewSession(runner)
	session.SetCodexFallback("auto")

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RoleEngineer1,
		Model:  "claude-sonnet-4-6",
		Prompt: "test",
	})

	require.NoError(t, err)
	assert.Equal(t, "codex answer from last-message file", result.Transcript)
}
