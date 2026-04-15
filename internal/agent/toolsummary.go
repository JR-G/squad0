package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolCallSummary counts tool invocations from a finished Claude Code
// stream so the orchestrator can log a one-line "did work happen?"
// summary at session end. This is the post-hoc visibility fallback
// for work sessions — squad0 captures stdout as a whole buffer rather
// than streaming it live, so there's no running commentary during a
// session. A summary at session end is the minimum fix that answers
// "was the engineer actually working?" without reading ~/.claude jsonl
// files by hand.
type ToolCallSummary struct {
	// Counts is a small fixed-form count of the commonly interesting
	// tools (Edit, Write, Read, Bash, Grep, Glob, TodoWrite). Any tool
	// that isn't in this set is bucketed into "other" so the summary
	// stays short.
	Counts map[string]int
	// Files is the set of file paths touched by Edit/Write. Used to
	// render a short example like "routes/projects.ts, app.ts (+3)"
	// in the summary line.
	Files []string
}

// SummariseToolCalls walks a stream of parsed Claude Code messages and
// counts tool_use blocks. Pure function — no IO, safe to call from the
// orchestrator once Run returns.
func SummariseToolCalls(messages []StreamMessage) ToolCallSummary {
	summary := ToolCallSummary{Counts: map[string]int{}}
	fileSet := map[string]struct{}{}

	for _, msg := range messages {
		if msg.Type != streamRoleAssistant || len(msg.Message) == 0 {
			continue
		}

		var parsed struct {
			Content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		}
		if err := json.Unmarshal(msg.Message, &parsed); err != nil {
			continue
		}

		for _, block := range parsed.Content {
			if block.Type != "tool_use" {
				continue
			}

			bucket := bucketForTool(block.Name)
			summary.Counts[bucket]++

			if path := extractFilePath(block.Input); path != "" {
				fileSet[path] = struct{}{}
			}
		}
	}

	summary.Files = make([]string, 0, len(fileSet))
	for path := range fileSet {
		summary.Files = append(summary.Files, path)
	}
	sort.Strings(summary.Files)
	return summary
}

// bucketForTool groups tools into the short set we care about. Any
// tool we don't recognise goes into "other" so the summary stays
// compact without dropping data.
func bucketForTool(name string) string {
	switch name {
	case "Edit", "Write", "Read", "Bash", "Grep", "Glob", "TodoWrite":
		return name
	default:
		if strings.HasPrefix(name, "mcp__") {
			return "mcp"
		}
		return "other"
	}
}

// extractFilePath pulls a file_path field out of the tool_use input
// JSON. Edit/Write/Read all use the same key name; Bash/Grep/Glob
// don't, and the function returns "" for them.
func extractFilePath(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var parsed struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return ""
	}
	return parsed.FilePath
}

// Format renders the summary as a one-line log message. Ordering is
// stable: the tools appear in a fixed priority order so summaries
// across sessions are easy to skim. Example:
//
//	"12 edits, 4 writes, 8 reads, 2 bashes · routes/projects.ts, app.ts (+3 more)"
func (summary ToolCallSummary) Format() string {
	if len(summary.Counts) == 0 {
		return "no tool calls"
	}

	order := []struct {
		bucket string
		label  string
	}{
		{"Edit", "edit"},
		{"Write", "write"},
		{"Read", "read"},
		{"Bash", "bash"},
		{"Grep", "grep"},
		{"Glob", "glob"},
		{"TodoWrite", "todo"},
		{"mcp", "mcp"},
		{"other", "other"},
	}

	parts := make([]string, 0, len(order))
	for _, entry := range order {
		count := summary.Counts[entry.bucket]
		if count == 0 {
			continue
		}
		parts = append(parts, pluralise(count, entry.label))
	}

	line := strings.Join(parts, ", ")
	if len(summary.Files) == 0 {
		return line
	}

	return line + " · " + formatFileList(summary.Files)
}

func pluralise(count int, label string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", label)
	}
	return fmt.Sprintf("%d %s", count, pluralForm(label))
}

// pluralForm returns the plural of a label using the minimal English
// rules this code actually needs — words ending in "sh", "ch", "x",
// or "s" take "es" ("bashes", not "bashs"), everything else takes a
// plain "s". Hand-rolled because the only irregular label in the
// summary is "bash" and pulling in a pluralisation library for one
// word is absurd.
func pluralForm(label string) string {
	if strings.HasSuffix(label, "sh") ||
		strings.HasSuffix(label, "ch") ||
		strings.HasSuffix(label, "x") ||
		strings.HasSuffix(label, "s") {
		return label + "es"
	}
	return label + "s"
}

// formatFileList shows up to 2 file basenames with "(+N more)" if
// there are additional paths. Full paths would bloat the log —
// basenames preserve the "where is this happening" signal without
// quoting 60-character monorepo paths.
func formatFileList(files []string) string {
	const maxShow = 2

	if len(files) == 0 {
		return ""
	}

	visible := files
	extra := 0
	if len(files) > maxShow {
		visible = files[:maxShow]
		extra = len(files) - maxShow
	}

	basenames := make([]string, 0, len(visible))
	for _, path := range visible {
		basenames = append(basenames, baseName(path))
	}

	result := strings.Join(basenames, ", ")
	if extra > 0 {
		result = fmt.Sprintf("%s (+%d more)", result, extra)
	}
	return result
}

func baseName(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
