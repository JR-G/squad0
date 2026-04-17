package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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

// commentFetcher loads structured review feedback for the approval
// gate. Overridable in tests via SetCommentFetcherForTest.
var commentFetcher = fetchStructuredComments

// liveBotReviewChecker reports whether a known review bot
// (Devin, CodeRabbit) left a COMMENTED review after the latest push
// to the PR branch. Overridable in tests via
// SetLiveBotReviewCheckerForTest.
var liveBotReviewChecker = hasLiveBotReview

// HasOutstandingReviewComments returns true if the PR has unaddressed
// BLOCKER-severity feedback that a plain reviewDecision check would
// miss — either structured blocker comments parsed from review
// bodies, or a live COMMENTED review from a known bot reviewer
// (Devin, CodeRabbit) posted after the most recent commit. Used to
// gate approval transitions so squad0 doesn't merge PRs with open
// review feedback.
//
// Suggestions are deliberately NOT counted. They are advisory by
// design (see FormatFixUpChecklist: "Suggestions are optional but
// appreciated") and must not block merge. A previous bug returned
// true on any comment at all, which trapped PRs in an infinite
// reviewing→fix-up→re-review loop whenever a reviewer used the word
// "suggestion" in an otherwise approved review — e.g. JAM-24.
func HasOutstandingReviewComments(ctx context.Context, repoDir, prURL string) bool {
	comments := commentFetcher(ctx, repoDir, prURL)
	for _, comment := range comments {
		if comment.Severity == severityBlocker {
			return true
		}
	}
	return liveBotReviewChecker(ctx, repoDir, prURL)
}

// SetCommentFetcherForTest replaces commentFetcher and returns a
// restore function so tests can drive the structured-comment branch
// deterministically without spawning gh.
func SetCommentFetcherForTest(fn func(context.Context, string, string) []ReviewComment) func() {
	prev := commentFetcher
	commentFetcher = fn
	return func() { commentFetcher = prev }
}

// SetLiveBotReviewCheckerForTest replaces liveBotReviewChecker and
// returns a restore function so tests can drive the bot-review
// branch deterministically without spawning gh.
func SetLiveBotReviewCheckerForTest(fn func(context.Context, string, string) bool) func() {
	prev := liveBotReviewChecker
	liveBotReviewChecker = fn
	return func() { liveBotReviewChecker = prev }
}

// hasLiveBotReview shells out to gh to check whether Devin or
// CodeRabbit has an active COMMENTED review on the PR head. A review
// is "live" when its submittedAt is after the last commit on the
// branch — once the engineer pushes a fix the review becomes stale
// and stops blocking the approval gate.
func hasLiveBotReview(ctx context.Context, repoDir, prURL string) bool {
	if repoDir == "" || prURL == "" || ctx.Err() != nil {
		return false
	}
	if _, err := os.Stat(repoDir + "/.git"); err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prURL, "--json", "reviews,commits")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return parseLiveBotReview(output)
}

// parseLiveBotReview reports whether the given gh JSON blob contains
// a COMMENTED review from a known bot that should still block merge.
//
// A bot review counts as live when:
//   - it was submitted after the latest commit on the PR branch
//     (engineer hasn't pushed a fix yet), AND
//   - no human reviewer has APPROVED the PR after the bot review
//     (a later human approval supersedes the bot's prior comment —
//     reviewers see the bot feedback, judge it, and either fix it
//     or signal it's not blocking by approving on top).
//
// The supersession rule fixes a permanent ping-pong loop where a
// bot review posted before the engineer's last commit (or about
// feedback addressed without a fresh commit) would block forever:
// the engineer never pushes again because there's nothing to fix,
// and the bot review stays "after the last commit" indefinitely.
// Seen in the wild on JAM-24.
//
// Pure function, tested directly.
func parseLiveBotReview(data []byte) bool {
	var parsed struct {
		Reviews []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submittedAt"`
		} `json:"reviews"`
		Commits []struct {
			CommittedDate time.Time `json:"committedDate"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false
	}
	if len(parsed.Commits) == 0 {
		return false
	}

	lastCommit := parsed.Commits[0].CommittedDate
	for _, commit := range parsed.Commits[1:] {
		if commit.CommittedDate.After(lastCommit) {
			lastCommit = commit.CommittedDate
		}
	}

	for _, review := range parsed.Reviews {
		if review.State != "COMMENTED" {
			continue
		}
		if !isBotReviewer(review.Author.Login) {
			continue
		}
		if !review.SubmittedAt.After(lastCommit) {
			continue
		}
		if humanApprovedAfter(parsed.Reviews, review.SubmittedAt) {
			continue
		}
		return true
	}
	return false
}

// humanApprovedAfter reports whether any human reviewer submitted an
// APPROVED review after the given time. "Human" is defined as
// not-isBotReviewer — anyone whose login isn't on the known-bot list
// counts, including squad0's own reviewer agents.
func humanApprovedAfter(reviews []struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}, after time.Time,
) bool {
	for _, review := range reviews {
		if review.State != "APPROVED" {
			continue
		}
		if isBotReviewer(review.Author.Login) {
			continue
		}
		if review.SubmittedAt.After(after) {
			return true
		}
	}
	return false
}

// isBotReviewer returns true for login handles belonging to known
// automated reviewers whose COMMENTED reviews should block merges.
func isBotReviewer(login string) bool {
	lower := strings.ToLower(login)
	return strings.Contains(lower, "devin") || strings.Contains(lower, "coderabbit")
}

// ParseLiveBotReviewForTest exports parseLiveBotReview for tests.
func ParseLiveBotReviewForTest(data []byte) bool {
	return parseLiveBotReview(data)
}

func fetchStructuredComments(ctx context.Context, repoDir, prURL string) []ReviewComment {
	if repoDir == "" || prURL == "" || ctx.Err() != nil {
		return nil
	}
	if _, err := os.Stat(repoDir + "/.git"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prURL, "--json", "reviews,comments",
		"--jq", `([.reviews[] | select(.state == "CHANGES_REQUESTED" or .state == "COMMENTED") | .body] + [.comments[] | select(.author.login != "vercel") | .body]) | .[]`)
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
