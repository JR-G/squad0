package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	severityBlocker    = "blocker"
	severitySuggestion = "suggestion"
)

// ReviewComment is a single review item extracted from a PR. Each gets
// a stable ID for tracking across the fix-up → re-review cycle.
type ReviewComment struct {
	ID       string // "rc-1", "rc-2", etc.
	Severity string // "blocker" or "suggestion"
	Body     string
	Path     string // File path, if file-specific.
	Resolved bool   // Set after diff verification.
}

// FetchReviewComments extracts review comments from a PR via gh CLI.
// Pure CLI + JSON parsing, no Claude session.

// ParseReviewBody extracts structured comments from a reviewer's body
// text. Looks for numbered items with [blocker] or [suggestion] tags.
func ParseReviewBody(body string) []ReviewComment {
	lines := strings.Split(body, "\n")
	comments := make([]ReviewComment, 0, len(lines))
	idx := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		severity, content := classifyLine(line)
		if severity == "" {
			continue
		}

		idx++
		comments = append(comments, ReviewComment{
			ID:       fmt.Sprintf("rc-%d", idx),
			Severity: severity,
			Body:     content,
			Path:     extractFilePath(content),
		})
	}

	return comments
}

func classifyLine(line string) (severity, content string) {
	lower := strings.ToLower(line)

	// Strip leading list markers: "1.", "2.", "-", "*"
	clean := strings.TrimLeft(line, "0123456789.-*) ")

	switch {
	case strings.Contains(lower, "[blocker]"):
		return severityBlocker, strings.Replace(clean, "[blocker]", "", 1)
	case strings.Contains(lower, "[suggestion]"):
		return severitySuggestion, strings.Replace(clean, "[suggestion]", "", 1)
	case strings.Contains(lower, "blocker"):
		return severityBlocker, clean
	default:
		return "", ""
	}
}

func extractFilePath(text string) string {
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, "`\"'(),;:")
		if path := stripLineNumber(word); looksLikeFilePath(path) {
			return path
		}
		if looksLikeFilePath(word) {
			return word
		}
	}
	return ""
}

// stripLineNumber removes a trailing :lineNumber (e.g. "auth.go:42" → "auth.go").
func stripLineNumber(word string) string {
	colonIdx := strings.LastIndex(word, ":")
	if colonIdx <= 0 {
		return word
	}
	return word[:colonIdx]
}

func looksLikeFilePath(word string) bool {
	extensions := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".sql", ".yaml", ".yml", ".toml"}
	for _, ext := range extensions {
		if strings.HasSuffix(word, ext) {
			return true
		}
	}
	return strings.Contains(word, "/") && len(word) > 3
}

// CheckCommentsAddressedWithDiff verifies comments against a diff file
// list. Used after fix-up to check which review comments were addressed.
func CheckCommentsAddressedWithDiff(comments []ReviewComment, diffOutput string) []ReviewComment {
	result := make([]ReviewComment, len(comments))
	copy(result, comments)

	for idx := range result {
		if result[idx].Path == "" {
			continue
		}
		if strings.Contains(diffOutput, result[idx].Path) {
			result[idx].Resolved = true
		}
	}

	return result
}

// FormatFixUpChecklist formats review comments as a numbered checklist
// for the fix-up prompt.
func FormatFixUpChecklist(comments []ReviewComment) string {
	if len(comments) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("## Review Comments to Address\n\n")

	for _, comment := range comments {
		prefix := "suggestion"
		if comment.Severity == "blocker" {
			prefix = "BLOCKER"
		}
		fmt.Fprintf(&builder, "- [%s] %s (%s)\n", comment.ID, strings.TrimSpace(comment.Body), prefix)
	}

	builder.WriteString("\nAddress ALL blockers. Suggestions are optional but appreciated.\n")
	return builder.String()
}

// FormatReReviewChecklist formats review comments with verification
// status for the re-reviewer.
func FormatReReviewChecklist(comments []ReviewComment) string {
	if len(comments) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("## Verification Checklist\n\n")

	for _, comment := range comments {
		status := "NOT ADDRESSED"
		if comment.Resolved {
			status = "ADDRESSED (file changed)"
		}
		fmt.Fprintf(&builder, "- [%s] %s — %s\n", comment.ID, strings.TrimSpace(comment.Body), status)
	}

	builder.WriteString("\nVerify ADDRESSED items are correct. Flag NOT ADDRESSED items.\n")
	return builder.String()
}

func fetchStructuredComments(ctx context.Context, repoDir, prURL string) []ReviewComment {
	if repoDir == "" || prURL == "" || ctx.Err() != nil {
		return nil
	}
	if _, err := os.Stat(repoDir + "/.git"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prURL, "--json", "reviews",
		"--jq", `.reviews[] | select(.state == "CHANGES_REQUESTED") | .body`)
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return ParseReviewBody(string(output))
}

// SummariseVerification returns a short summary of how many blocker
// comments were verified in the diff.
func SummariseVerification(comments []ReviewComment) string {
	if len(comments) == 0 {
		return ""
	}
	resolved := 0
	total := 0
	for _, comment := range comments {
		if comment.Severity != severityBlocker {
			continue
		}
		total++
		if comment.Resolved {
			resolved++
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("(%d/%d blockers verified in diff)", resolved, total)
}
