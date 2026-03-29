package logging

import (
	"fmt"
	"io"
	"strings"
	"sync"
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

	catError = "error"
	catInfo  = "info"
)

// ConsoleWriter is an io.Writer that adds colour and structure to log
// output. Designed to be set as the output for Go's standard logger.
type ConsoleWriter struct {
	out    io.Writer
	mu     sync.Mutex
	roster map[string]string // role → name
}

// NewConsoleWriter creates a coloured console writer.
func NewConsoleWriter(out io.Writer) *ConsoleWriter {
	return &ConsoleWriter{out: out, roster: make(map[string]string)}
}

// SetRoster provides agent names so logs show "Mara" instead of "engineer-2".
func (cw *ConsoleWriter) SetRoster(roster map[string]string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.roster = roster
}

// Write implements io.Writer. Parses standard log format and adds colour.
func (cw *ConsoleWriter) Write(data []byte) (int, error) {
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		return len(data), nil
	}

	msg = stripLogTimestamp(msg)

	// Suppress noisy lines.
	if shouldSuppress(msg) {
		return len(data), nil
	}

	ts := dim + time.Now().Format("15:04:05") + reset
	category, body := extractCategory(msg)
	icon := categoryIcon(category)
	colour := categoryColour(category)

	cw.mu.Lock()
	body = cw.replaceRoles(body)
	cw.mu.Unlock()

	formatted := fmt.Sprintf("%s %s%s %-8s%s %s\n", ts, colour, icon, category, reset, body)
	_, err := cw.out.Write([]byte(formatted))
	return len(data), err
}

func shouldSuppress(msg string) bool {
	return strings.HasPrefix(strings.ToLower(msg), "tick: work_enabled=")
}

func stripLogTimestamp(msg string) string {
	if len(msg) > 20 && msg[4] == '/' && msg[7] == '/' && msg[10] == ' ' {
		return msg[20:]
	}
	return msg
}

func extractCategory(msg string) (category, body string) {
	lower := strings.ToLower(msg)

	rules := []struct {
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
		{"own pr check:", "own-pr"},
		{"orchestrator", "system"},
		{"socket event:", "socket"},
		{"chat:", "chat"},
		{"message received:", "slack"},
		{"work item", "pipeline"},
		{"engineer merge", "merge"},
		{"session error", catError},
		{"rescue pr", "rescue"},
		{"pm said:", "assign"},
		{"rebase session", "rebase"},
	}

	for _, rule := range rules {
		matched := strings.HasPrefix(lower, rule.prefix) || strings.Contains(lower, rule.prefix)
		if !matched {
			continue
		}
		body = msg
		if strings.HasPrefix(lower, rule.prefix) {
			body = strings.TrimSpace(msg[len(rule.prefix):])
		}
		return rule.category, body
	}

	// Error detection as a fallback — check for failure keywords.
	if strings.Contains(lower, "failed") || strings.Contains(lower, "error") {
		return catError, msg
	}

	return catInfo, msg
}

func categoryIcon(category string) string {
	switch category {
	case "system":
		return "●"
	case "tick":
		return "↻"
	case "assign":
		return "→"
	case "resume", "pipeline":
		return "↺"
	case "review":
		return "◉"
	case "merge":
		return "✓"
	case "rebase":
		return "⟳"
	case "idle", "own-pr":
		return "◌"
	case "chat":
		return "◆"
	case "slack":
		return "◇"
	case "socket":
		return "⋯"
	case "error":
		return "✗"
	case "fixup":
		return "↩"
	case "rescue":
		return "⚑"
	default:
		return "·"
	}
}

func categoryColour(category string) string {
	switch category {
	case "tick":
		return dim
	case "system":
		return bold + blue
	case "assign":
		return bold + cyan
	case "resume", "pipeline":
		return cyan
	case "review":
		return magenta
	case "merge":
		return bold + green
	case "rebase":
		return yellow
	case "idle", "own-pr":
		return dim + cyan
	case "chat":
		return yellow
	case "slack":
		return dim + yellow
	case "socket":
		return dim
	case "error":
		return bold + red
	case "fixup":
		return yellow
	case "rescue":
		return magenta
	default:
		return white
	}
}

func (cw *ConsoleWriter) replaceRoles(body string) string {
	roles := []string{
		"engineer-1", "engineer-2", "engineer-3",
		"tech-lead", "reviewer", "designer", "pm",
	}
	for _, role := range roles {
		if !strings.Contains(body, role) {
			continue
		}
		name := cw.roster[role]
		if name == "" || name == role {
			body = strings.ReplaceAll(body, role, bold+role+reset)
			continue
		}
		body = strings.ReplaceAll(body, role, bold+name+reset+dim+" ("+role+")"+reset)
	}
	return body
}
