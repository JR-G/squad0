// Package cli is a ports.PullRequestHost adapter that shells out to
// the `gh` CLI. The orchestrator depends on the port, not on this
// adapter — swapping in the go-github library or a Bitbucket adapter
// is a constructor change, not a code change.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/JR-G/squad0/internal/ports"
)

// Runner executes the `gh` CLI; pure interface so tests can inject
// a fake without spawning a real process.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// Client wraps `gh pr` invocations behind ports.PullRequestHost.
type Client struct {
	repoDir string
	runner  Runner
}

// NewClient binds the adapter to a working directory `gh` should
// run in. The directory's git remote determines which repo gh
// targets.
func NewClient(repoDir string) *Client {
	return &Client{repoDir: repoDir, runner: execRunner{repoDir: repoDir}}
}

// NewClientWithRunner is the testing constructor — pass a Runner
// fake so the adapter never spawns a real process.
func NewClientWithRunner(runner Runner) *Client {
	return &Client{runner: runner}
}

type execRunner struct{ repoDir string }

func (r execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if r.repoDir != "" {
		cmd.Dir = r.repoDir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %v: %w", args, err)
	}
	return output, nil
}

// State returns the current PR state. See ports.PullRequestHost.
func (client *Client) State(ctx context.Context, prURL string) (ports.PRState, error) {
	output, err := client.gh(ctx, "pr", "view", prURL, "--json", "state,reviewDecision,mergeable,headRefName,baseRefName,updatedAt,url")
	if err != nil {
		return ports.PRState{}, err
	}

	var raw struct {
		URL            string `json:"url"`
		State          string `json:"state"`
		ReviewDecision string `json:"reviewDecision"`
		Mergeable      string `json:"mergeable"`
		HeadRefName    string `json:"headRefName"`
		BaseRefName    string `json:"baseRefName"`
		UpdatedAt      string `json:"updatedAt"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return ports.PRState{}, fmt.Errorf("parsing PR state for %s: %w", prURL, err)
	}

	state := ports.PRState{
		URL: raw.URL, State: raw.State, ReviewDecision: raw.ReviewDecision,
		Mergeable: raw.Mergeable, HeadRefName: raw.HeadRefName, BaseRefName: raw.BaseRefName,
	}
	if raw.UpdatedAt != "" {
		_ = state.UpdatedAt.UnmarshalText([]byte(raw.UpdatedAt))
	}
	return state, nil
}

// Reviews returns the PR's reviews. See ports.PullRequestHost.
func (client *Client) Reviews(ctx context.Context, prURL string) ([]ports.PRReview, error) {
	output, err := client.gh(ctx, "pr", "view", prURL, "--json", "reviews")
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Reviews []struct {
			Author      struct{ Login string } `json:"author"`
			State       string                 `json:"state"`
			Body        string                 `json:"body"`
			SubmittedAt string                 `json:"submittedAt"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(output, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing reviews for %s: %w", prURL, err)
	}

	reviews := make([]ports.PRReview, 0, len(wrapper.Reviews))
	for _, item := range wrapper.Reviews {
		review := ports.PRReview{AuthorLogin: item.Author.Login, State: item.State, Body: item.Body}
		_ = review.SubmittedAt.UnmarshalText([]byte(item.SubmittedAt))
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// Comments returns issue-level (non-review) comments on the PR.
func (client *Client) Comments(ctx context.Context, prURL string) ([]ports.PRComment, error) {
	output, err := client.gh(ctx, "pr", "view", prURL, "--json", "comments")
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Comments []struct {
			Author    struct{ Login string } `json:"author"`
			Body      string                 `json:"body"`
			CreatedAt string                 `json:"createdAt"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(output, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing comments for %s: %w", prURL, err)
	}

	comments := make([]ports.PRComment, 0, len(wrapper.Comments))
	for _, item := range wrapper.Comments {
		comment := ports.PRComment{AuthorLogin: item.Author.Login, Body: item.Body}
		_ = comment.CreatedAt.UnmarshalText([]byte(item.CreatedAt))
		comments = append(comments, comment)
	}
	return comments, nil
}

// Commits returns the commits on the PR's branch.
func (client *Client) Commits(ctx context.Context, prURL string) ([]ports.PRCommit, error) {
	output, err := client.gh(ctx, "pr", "view", prURL, "--json", "commits")
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Commits []struct {
			OID           string `json:"oid"`
			CommittedDate string `json:"committedDate"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(output, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing commits for %s: %w", prURL, err)
	}

	commits := make([]ports.PRCommit, 0, len(wrapper.Commits))
	for _, item := range wrapper.Commits {
		commit := ports.PRCommit{SHA: item.OID}
		_ = commit.CommittedDate.UnmarshalText([]byte(item.CommittedDate))
		commits = append(commits, commit)
	}
	return commits, nil
}

// List returns PRs matching the filter.
func (client *Client) List(ctx context.Context, filter ports.PRListFilter) ([]ports.PRListing, error) {
	args := []string{"pr", "list", "--json", "url,state,title,number"}
	if filter.State != "" {
		args = append(args, "--state", filter.State)
	}
	if filter.Limit > 0 {
		args = append(args, "--limit", fmt.Sprintf("%d", filter.Limit))
	}

	output, err := client.gh(ctx, args...)
	if err != nil {
		return nil, err
	}

	var listings []ports.PRListing
	if err := json.Unmarshal(output, &listings); err != nil {
		return nil, fmt.Errorf("parsing PR list: %w", err)
	}
	return listings, nil
}

// Comment posts an issue-level comment on the PR.
func (client *Client) Comment(ctx context.Context, prURL, body string) error {
	_, err := client.gh(ctx, "pr", "comment", prURL, "--body", body)
	return err
}

// Merge squash-merges the PR.
func (client *Client) Merge(ctx context.Context, prURL string) error {
	_, err := client.gh(ctx, "pr", "merge", prURL, "--squash")
	return err
}

func (client *Client) gh(ctx context.Context, args ...string) ([]byte, error) {
	return client.runner.Run(ctx, args...)
}
