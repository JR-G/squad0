package logging

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// ANSI colour codes.
const (
	reset   = "\033[0m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
	bold    = "\033[1m"
)

// ConsoleWriter is an io.Writer that adds colour and structure to log
// output. Designed to be set as the output for Go's standard logger.
type ConsoleWriter struct {
	out io.Writer
}

// NewConsoleWriter creates a coloured console writer.
func NewConsoleWriter(out io.Writer) *ConsoleWriter {
	return &ConsoleWriter{out: out}
}

// Write implements io.Writer. Parses standard log format and adds colour.
func (cw *ConsoleWriter) Write(data []byte) (int, error) {
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		return len(data), nil
	}

	// Strip the standard log timestamp prefix (e.g. "2026/03/27 15:07:33 ").
	msg = stripLogTimestamp(msg)

	ts := dim + time.Now().Format("15:04:05") + reset
	category, body := extractCategory(msg)
	colour := categoryColour(category)

	formatted := fmt.Sprintf("%s %s%-10s%s %s\n", ts, colour, category, reset, colourBody(body))
	_, err := cw.out.Write([]byte(formatted))
	return len(data), err
}

func stripLogTimestamp(msg string) string {
	// Standard log format: "2006/01/02 15:04:05 message"
	if len(msg) > 20 && msg[4] == '/' && msg[7] == '/' && msg[10] == ' ' {
		return msg[20:]
	}
	return msg
}

func extractCategory(msg string) (category, body string) {
	lower := strings.ToLower(msg)

	categories := []struct {
		prefix   string
		category string
	}{
		{"tick:", "tick"},
		{"resume:", "resume"},
		{"resuming", "resume"},
		{"review:", "review"},
		{"re-review:", "review"},
		{"fix-up:", "fixup"},
		{"merge:", "merge"},
		{"idle duty:", "idle"},
		{"own pr check:", "pr-check"},
		{"orchestrator", "system"},
		{"socket event:", "socket"},
		{"worktree", "worktree"},
		{"chat:", "chat"},
		{"message received:", "slack"},
		{"work item", "pipeline"},
		{"engineer merge", "merge"},
		{"session error", "error"},
		{"failed", "error"},
	}

	for _, cat := range categories {
		matched := strings.HasPrefix(lower, cat.prefix) || strings.Contains(lower, cat.prefix)
		if !matched {
			continue
		}
		body = msg
		if strings.HasPrefix(lower, cat.prefix) {
			body = strings.TrimSpace(msg[len(cat.prefix):])
		}
		return cat.category, body
	}

	return "info", msg
}

func categoryColour(category string) string {
	switch category {
	case "tick":
		return dim
	case "system":
		return bold + blue
	case "resume", "pipeline":
		return cyan
	case "review":
		return magenta
	case "merge":
		return green
	case "idle", "pr-check":
		return dim + cyan
	case "chat":
		return yellow
	case "slack":
		return dim + yellow
	case "socket":
		return dim
	case "error":
		return red
	case "fixup":
		return yellow
	case "worktree":
		return dim + blue
	default:
		return white
	}
}

func colourBody(body string) string {
	// Highlight agent names/roles in the body.
	roles := []string{
		"engineer-1", "engineer-2", "engineer-3",
		"tech-lead", "reviewer", "designer", "pm",
	}
	result := body
	for _, role := range roles {
		if strings.Contains(result, role) {
			result = strings.ReplaceAll(result, role, bold+role+reset)
		}
	}
	return result
}
