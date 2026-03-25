package orchestrator

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/pipeline"
)

const maxReviewCycles = 3

const reviewPromptTemplate = `You are reviewing a pull request for ticket %s.

## PR
%s

## Instructions
1. Read the PR diff: gh pr diff %s
2. Read the PR description: gh pr view %s
3. Check for review comments (CodeRabbit, humans, prior reviews): gh pr view %s --comments
4. Analyse the changes for:
   - Correctness: does the code do what the ticket asks?
   - Bugs: off-by-one errors, nil pointer dereferences, race conditions
   - Tests: are the changes adequately tested?
   - Style: does it follow the project's conventions?
   - Security: any injection, XSS, or auth issues?

5. Post your review as a GitHub PR review:
   If approved: gh pr review %s --approve --body "your summary"
   If changes needed: gh pr review %s --request-changes --body "your feedback"

Be constructive and specific. Reference line numbers. If it looks good, approve it.
End your response with either APPROVED or CHANGES_REQUESTED on its own line.
`

const fixUpPromptTemplate = `You need to address review feedback on your PR for ticket %s.

## PR
%s

## Instructions
1. Read the review comments: gh pr view %s --comments
2. Read the current diff: gh pr diff %s
3. Address every piece of feedback — fix the code, update tests, handle edge cases
4. Commit your fixes with conventional commit messages
5. Push to the same branch — do NOT create a new PR

Focus on what the reviewer asked for. Don't refactor unrelated code.
`

var prURLPattern = regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/pull/\d+`)

// ReviewOutcome classifies what the reviewer decided.
type ReviewOutcome string

const (
	// ReviewApproved means the PR passed review.
	ReviewApproved ReviewOutcome = "approved"
	// ReviewChangesRequested means the reviewer wants fixes.
	ReviewChangesRequested ReviewOutcome = "changes_requested"
)

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
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(reviewPromptTemplate, ticket, prURL, prNum, prNum, prNum, prNum, prNum)
}

// BuildFixUpPrompt creates the prompt for an engineer to address review feedback.
func BuildFixUpPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(fixUpPromptTemplate, ticket, prURL, prNum, prNum)
}

// ClassifyReviewOutcome scans the reviewer's transcript for approval
// or change request signals. Defaults to approved to avoid blocking.
func ClassifyReviewOutcome(transcript string) ReviewOutcome {
	upper := strings.ToUpper(transcript)

	changeSignals := []string{"CHANGES_REQUESTED", "REQUEST CHANGES", "NEEDS CHANGES", "PLEASE FIX"}
	for _, signal := range changeSignals {
		if strings.Contains(upper, signal) {
			return ReviewChangesRequested
		}
	}

	return ReviewApproved
}

// StartReviewForTest is an exported wrapper for testing.
func (orch *Orchestrator) StartReviewForTest(ctx context.Context, prURL, ticket string) {
	orch.startReview(ctx, prURL, ticket, 0, "")
}

// StartReviewWithItemForTest is an exported wrapper that passes
// work item ID and engineer role for testing the feedback loop.
func (orch *Orchestrator) StartReviewWithItemForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.startReview(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) startReview(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent available for PR review")
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageReviewing)

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

		orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole)
	}()
}

func (orch *Orchestrator) runReview(ctx context.Context, reviewer *agent.Agent, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	prompt := BuildReviewPrompt(prURL, ticket)

	result, err := reviewer.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("review session failed for %s: %v", ticket, err)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Couldn't complete review for %s — will try again later", ticket),
			agent.RoleReviewer)
		return
	}

	outcome := ClassifyReviewOutcome(result.Transcript)
	summary := agent.TruncateSummary(result.Transcript, 300)

	switch outcome {
	case ReviewApproved:
		orch.advancePipeline(ctx, workItemID, pipeline.StageApproved)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Approved %s: %s", ticket, summary),
			agent.RoleReviewer)

	case ReviewChangesRequested:
		orch.handleChangesRequested(ctx, prURL, ticket, workItemID, engineerRole, summary)
	}
}

func (orch *Orchestrator) handleChangesRequested(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role, reviewSummary string) {
	orch.advancePipeline(ctx, workItemID, pipeline.StageChangesRequested)

	if orch.shouldEscalate(ctx, workItemID, ticket) {
		return
	}

	orch.announceAsRole(ctx, "reviews",
		fmt.Sprintf("Changes requested on %s: %s", ticket, reviewSummary),
		agent.RoleReviewer)

	orch.startFixUp(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) startFixUp(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	engineerAgent, ok := orch.agents[engineerRole]
	if !ok {
		log.Printf("no agent for %s to address review feedback", engineerRole)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageWorking)

	prompt := BuildFixUpPrompt(prURL, ticket)

	result, err := engineerAgent.ExecuteTask(ctx, prompt, nil, orch.cfg.TargetRepoDir)
	if err != nil {
		log.Printf("fix-up session failed for %s on %s: %v", engineerRole, ticket, err)
		return
	}

	_ = result
	orch.startReview(ctx, prURL, ticket, workItemID, engineerRole)
}
