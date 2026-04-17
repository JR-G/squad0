package ports

import (
	"context"
	"time"
)

// PRState captures the subset of GitHub PR fields squad0's
// orchestrator reasons about during reconciliation.
type PRState struct {
	URL            string
	State          string // "OPEN" | "CLOSED" | "MERGED"
	ReviewDecision string // "APPROVED" | "CHANGES_REQUESTED" | "REVIEW_REQUIRED" | ""
	Mergeable      string // "MERGEABLE" | "CONFLICTING" | "UNKNOWN"
	HeadRefName    string
	BaseRefName    string
	UpdatedAt      time.Time
}

// PRReview is one review on a pull request — used by the bot-
// supersession check in reviewcomments.go.
type PRReview struct {
	AuthorLogin string
	State       string // "APPROVED" | "CHANGES_REQUESTED" | "COMMENTED"
	SubmittedAt time.Time
	Body        string
}

// PRComment is an issue-level comment on a PR (not a code review).
type PRComment struct {
	AuthorLogin string
	Body        string
	CreatedAt   time.Time
}

// PRCommit summarises one commit on a PR's branch — used to decide
// whether a bot review is "live" (after the latest commit).
type PRCommit struct {
	SHA           string
	CommittedDate time.Time
}

// PRListFilter scopes a list query — empty string fields mean "no
// filter on this dimension".
type PRListFilter struct {
	State string // "open" | "closed" | "merged" | "all" | ""
	Limit int    // 0 = no limit / use provider default
}

// PRListing is a lightweight summary returned from List operations.
type PRListing struct {
	URL    string
	State  string
	Title  string
	Number int
}

// PullRequestHost is the contract for operations against a code-
// review host (GitHub today; could be GitLab / Gitea tomorrow).
//
// Implementations should be safe for concurrent use; callers do not
// serialise calls.
type PullRequestHost interface {
	// State fetches the current PR state. Returns an error when the
	// PR cannot be loaded (network failure, deleted, etc.).
	State(ctx context.Context, prURL string) (PRState, error)

	// Reviews lists all reviews on the PR, newest first.
	Reviews(ctx context.Context, prURL string) ([]PRReview, error)

	// Comments lists all issue-level (non-review) comments on the PR.
	Comments(ctx context.Context, prURL string) ([]PRComment, error)

	// Commits lists commits on the PR branch.
	Commits(ctx context.Context, prURL string) ([]PRCommit, error)

	// List returns a summary of PRs matching the filter.
	List(ctx context.Context, filter PRListFilter) ([]PRListing, error)

	// Comment posts an issue-level comment on the PR.
	Comment(ctx context.Context, prURL, body string) error

	// Merge merges the PR using the host's "squash" semantics.
	Merge(ctx context.Context, prURL string) error
}
