package orchestrator

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
)

const reviewPromptTemplate = `You are reviewing a pull request for ticket %s.

## PR
%s

## Instructions
1. Read the PR diff: gh pr diff %s
2. Read the PR description: gh pr view %s
3. Check for automated review comments: gh pr view %s --comments
4. Analyse the changes for:
   - Correctness: does the code do what the ticket asks?
   - Bugs: off-by-one errors, nil pointer dereferences, race conditions
   - Tests: are the changes adequately tested?
   - Style: does it follow the project's conventions?
   - Security: any injection, XSS, or auth issues?

5. Post your review as a GitHub comment:
   gh pr comment %s --body "your review here"

Keep your review constructive and specific. Reference line numbers when pointing out issues. If the PR looks good, say so briefly.
`

var prURLPattern = regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/pull/\d+`)

// ExtractPRURL finds a GitHub pull request URL in the given text.
func ExtractPRURL(text string) string {
	return prURLPattern.FindString(text)
}

// ExtractPRNumber returns the PR number from a GitHub PR URL.
func ExtractPRNumber(prURL string) string {
	idx := strings.LastIndex(prURL, "/")
	if idx == -1 {
		return ""
	}
	return prURL[idx+1:]
}

// BuildReviewPrompt creates the prompt for a reviewer session.
func BuildReviewPrompt(prURL, ticket string) string {
	prNumber := ExtractPRNumber(prURL)
	return fmt.Sprintf(reviewPromptTemplate, ticket, prURL, prNumber, prNumber, prNumber, prNumber)
}

// StartReviewForTest is an exported wrapper for testing.
func (orch *Orchestrator) StartReviewForTest(ctx context.Context, prURL, ticket string) {
	orch.startReview(ctx, prURL, ticket)
}

// startReview assigns the Reviewer agent to review the given PR.
func (orch *Orchestrator) startReview(ctx context.Context, prURL, ticket string) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent available for PR review")
		return
	}

	err := orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleReviewer,
		Ticket:        ticket,
		Status:        coordination.StatusReviewing,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("reviewing PR for %s", ticket),
	})
	if err != nil {
		log.Printf("failed to update reviewer check-in: %v", err)
		return
	}

	orch.wg.Add(1)
	go func() {
		defer orch.wg.Done()
		defer func() { _ = orch.checkIns.SetIdle(ctx, agent.RoleReviewer) }()

		orch.runReview(ctx, reviewer, prURL, ticket)
	}()
}

func (orch *Orchestrator) runReview(ctx context.Context, reviewer *agent.Agent, prURL, ticket string) {
	prompt := BuildReviewPrompt(prURL, ticket)

	result, err := reviewer.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("review session failed for %s: %v", ticket, err)
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("Couldn't complete review for %s — will try again later", ticket),
			agent.RoleReviewer)
		return
	}

	summary := agent.TruncateSummary(result.Transcript, 300)
	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("Reviewed PR for %s: %s", ticket, summary),
		agent.RoleReviewer)
}
