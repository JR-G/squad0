package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
)

// buildAssistantMessage constructs a StreamMessage shaped like a real
// stream-json assistant message with tool_use blocks, so each table
// entry can exercise the SummariseToolCalls parser without hand-
// editing raw JSON in every case.
func buildAssistantMessage(t *testing.T, blocks ...map[string]any) agent.StreamMessage {
	t.Helper()
	payload := map[string]any{"content": blocks}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return agent.StreamMessage{Type: "assistant", Message: raw}
}

func toolUseBlock(name string, input map[string]any) map[string]any {
	block := map[string]any{"type": "tool_use", "name": name}
	if input != nil {
		block["input"] = input
	}
	return block
}

func TestSummariseToolCalls_CountsKnownBucketsAndTracksFiles(t *testing.T) {
	t.Parallel()

	messages := []agent.StreamMessage{
		buildAssistantMessage(t,
			toolUseBlock("Edit", map[string]any{"file_path": "/repo/a.go"}),
			toolUseBlock("Edit", map[string]any{"file_path": "/repo/b.go"}),
			toolUseBlock("Write", map[string]any{"file_path": "/repo/c.go"}),
			toolUseBlock("Read", map[string]any{"file_path": "/repo/a.go"}),
		),
		buildAssistantMessage(t,
			toolUseBlock("Bash", map[string]any{"command": "go test"}),
			toolUseBlock("Bash", map[string]any{"command": "go build"}),
			toolUseBlock("Grep", map[string]any{"pattern": "TODO"}),
			toolUseBlock("Glob", map[string]any{"pattern": "**/*.go"}),
			toolUseBlock("TodoWrite", map[string]any{"todos": []any{}}),
		),
	}

	summary := agent.SummariseToolCalls(messages)

	assert.Equal(t, 2, summary.Counts["Edit"])
	assert.Equal(t, 1, summary.Counts["Write"])
	assert.Equal(t, 1, summary.Counts["Read"])
	assert.Equal(t, 2, summary.Counts["Bash"])
	assert.Equal(t, 1, summary.Counts["Grep"])
	assert.Equal(t, 1, summary.Counts["Glob"])
	assert.Equal(t, 1, summary.Counts["TodoWrite"])
	// Files from Edit/Write/Read, deduplicated, sorted.
	assert.Equal(t, []string{"/repo/a.go", "/repo/b.go", "/repo/c.go"}, summary.Files)
}

func TestSummariseToolCalls_BucketsMCPAndUnknownTools(t *testing.T) {
	t.Parallel()

	messages := []agent.StreamMessage{
		buildAssistantMessage(t,
			toolUseBlock("mcp__claude_ai_Linear__get_issue", map[string]any{"id": "JAM-1"}),
			toolUseBlock("mcp__claude_ai_Linear__save_issue", map[string]any{"id": "JAM-1"}),
			toolUseBlock("SomeFutureTool", nil),
		),
	}

	summary := agent.SummariseToolCalls(messages)

	assert.Equal(t, 2, summary.Counts["mcp"])
	assert.Equal(t, 1, summary.Counts["other"])
	assert.Empty(t, summary.Files)
}

func TestSummariseToolCalls_IgnoresNonAssistantAndBadJSON(t *testing.T) {
	t.Parallel()

	messages := []agent.StreamMessage{
		// Not an assistant message — should be skipped entirely.
		{Type: "user", Message: json.RawMessage(`{"content":[{"type":"tool_use","name":"Edit"}]}`)},
		// Empty message — should be skipped, no panic.
		{Type: "assistant"},
		// Malformed JSON in Message — should be skipped, no panic.
		{Type: "assistant", Message: json.RawMessage(`{not json`)},
		// A real assistant message with no tool_use blocks — counts stay zero.
		buildAssistantMessage(t, map[string]any{"type": "text", "text": "hello"}),
	}

	summary := agent.SummariseToolCalls(messages)
	assert.Empty(t, summary.Counts)
	assert.Empty(t, summary.Files)
}

func TestSummariseToolCalls_InputWithoutFilePath_IgnoredForFiles(t *testing.T) {
	t.Parallel()

	messages := []agent.StreamMessage{
		buildAssistantMessage(t,
			toolUseBlock("Edit", map[string]any{"other_field": "no file_path here"}),
			toolUseBlock("Edit", map[string]any{}),
		),
	}

	summary := agent.SummariseToolCalls(messages)
	assert.Equal(t, 2, summary.Counts["Edit"])
	assert.Empty(t, summary.Files)
}

func TestToolCallSummary_Format_NoCalls(t *testing.T) {
	t.Parallel()

	var summary agent.ToolCallSummary
	summary.Counts = map[string]int{}
	assert.Equal(t, "no tool calls", summary.Format())
}

func TestToolCallSummary_Format_PluralisesAndOrdersAndTrims(t *testing.T) {
	t.Parallel()

	summary := agent.ToolCallSummary{
		Counts: map[string]int{
			"Edit":      12,
			"Write":     1,
			"Read":      8,
			"Bash":      2,
			"TodoWrite": 3,
			"other":     4,
		},
		// Files passed in caller-sorted order (SummariseToolCalls
		// always sorts before returning). formatFileList takes the
		// first 2 as-is.
		Files: []string{
			"/repo/apps/api/src/app.ts",
			"/repo/apps/api/src/queues/build.test.ts",
			"/repo/apps/api/src/routes/contributions.ts",
			"/repo/apps/api/src/routes/projects.ts",
		},
	}

	formatted := summary.Format()

	// Buckets appear in priority order regardless of map iteration.
	editIdx := indexOf(formatted, "12 edits")
	writeIdx := indexOf(formatted, "1 write")
	readIdx := indexOf(formatted, "8 reads")
	bashIdx := indexOf(formatted, "2 bashes")
	todoIdx := indexOf(formatted, "3 todos")
	otherIdx := indexOf(formatted, "4 others")

	assert.Less(t, editIdx, writeIdx, "edits before writes")
	assert.Less(t, writeIdx, readIdx, "writes before reads")
	assert.Less(t, readIdx, bashIdx, "reads before bash")
	assert.Less(t, bashIdx, todoIdx, "bash before todo")
	assert.Less(t, todoIdx, otherIdx, "todo before other")

	// Only the first two basenames are shown, remainder bucketed.
	assert.Contains(t, formatted, "app.ts")
	assert.Contains(t, formatted, "build.test.ts")
	assert.Contains(t, formatted, "(+2 more)")
}

func TestToolCallSummary_Format_SingleCountsSingularLabel(t *testing.T) {
	t.Parallel()

	summary := agent.ToolCallSummary{
		Counts: map[string]int{"Read": 1, "Grep": 1, "Glob": 1, "mcp": 1, "Bash": 1},
	}

	formatted := summary.Format()
	assert.Contains(t, formatted, "1 read")
	assert.NotContains(t, formatted, "1 reads")
	assert.Contains(t, formatted, "1 grep")
	assert.Contains(t, formatted, "1 glob")
	assert.Contains(t, formatted, "1 mcp")
	// Bash is singular here — the "es" plural only applies for count > 1.
	assert.Contains(t, formatted, "1 bash")
	assert.NotContains(t, formatted, "1 bashes")
}

func TestToolCallSummary_Format_FilesOnlyShownWhenPresent(t *testing.T) {
	t.Parallel()

	summary := agent.ToolCallSummary{
		Counts: map[string]int{"Bash": 5},
	}
	assert.Equal(t, "5 bashes", summary.Format())
}

func TestToolCallSummary_Format_RendersSingleBasename(t *testing.T) {
	t.Parallel()

	summary := agent.ToolCallSummary{
		Counts: map[string]int{"Edit": 1},
		Files:  []string{"README.md"},
	}

	formatted := summary.Format()
	assert.Contains(t, formatted, "README.md")
	assert.NotContains(t, formatted, "more")
}

func TestSummariseToolCalls_NilInputJSON_TrackedAsCallNotFile(t *testing.T) {
	t.Parallel()

	messages := []agent.StreamMessage{
		buildAssistantMessage(t,
			toolUseBlock("Edit", nil),
		),
	}

	summary := agent.SummariseToolCalls(messages)
	assert.Equal(t, 1, summary.Counts["Edit"])
	assert.Empty(t, summary.Files)
}

func TestSummariseToolCalls_MalformedInputJSON_NoFileTracked(t *testing.T) {
	t.Parallel()

	// Hand-craft a tool_use with broken input JSON so extractFilePath
	// hits the unmarshal-error branch.
	msg := agent.StreamMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"content":[{"type":"tool_use","name":"Edit","input":"not an object"}]}`),
	}

	summary := agent.SummariseToolCalls([]agent.StreamMessage{msg})
	assert.Equal(t, 1, summary.Counts["Edit"])
	assert.Empty(t, summary.Files)
}

func TestToolCallSummary_Format_EmptyFiles_NoSeparator(t *testing.T) {
	t.Parallel()

	summary := agent.ToolCallSummary{
		Counts: map[string]int{"Grep": 3},
		Files:  []string{},
	}
	assert.Equal(t, "3 greps", summary.Format())
	assert.NotContains(t, summary.Format(), "·")
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
