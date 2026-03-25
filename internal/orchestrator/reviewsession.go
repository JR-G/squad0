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

const reviewPromptTemplate = `You are reviewing pull request #%s for ticket %s.

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

5. You MUST submit your review using one of these gh commands — this is MANDATORY:
   To approve: gh pr review %s --approve --body "your review summary"
   To request changes: gh pr review %s --request-changes --body "your detailed feedback"

   IMPORTANT RULES:
   - Do NOT say "approved with comments" — that is NOT an approval.
   - If you have ANY concerns, use --request-changes. Don't be afraid to request changes.
   - The pipeline handles the fix-up loop automatically — requesting changes is normal.
   - Either fully approve or request changes. No middle ground.
   - You MUST actually run the gh pr review command, not just say "approved".

6. After submitting the review, verify it worked: gh pr view %s --json reviewDecision

End your response with either APPROVED or CHANGES_REQUESTED on its own line.
`

const reReviewPromptTemplate = `You previously reviewed PR #%s for ticket %s and requested changes.
The engineer has pushed fixes. Check that your specific concerns were addressed.

## PR
%s

## Instructions
1. Read your previous review comments: gh pr view %s --comments
2. Read the latest diff: gh pr diff %s
3. For EACH concern you raised previously, verify it was addressed:
   - Was the fix correct?
   - Were tests added or updated?
   - Did the fix introduce new issues?

4. Submit your review:
   If ALL concerns were addressed: gh pr review %s --approve --body "All feedback addressed"
   If concerns remain: gh pr review %s --request-changes --body "specific remaining issues"

5. Verify the review: gh pr view %s --json reviewDecision

Focus on YOUR previous comments specifically — don't re-review the entire diff.
End with APPROVED or CHANGES_REQUESTED.
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
	return fmt.Sprintf(reviewPromptTemplate, prNum, ticket, prURL, prNum, prNum, prNum, prNum, prNum, prNum)
}

// BuildReReviewPrompt creates the prompt for re-reviewing after fixes.
func BuildReReviewPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(reReviewPromptTemplate, prNum, ticket, prURL, prNum, prNum, prNum, prNum, prNum)
}

// BuildFixUpPrompt creates the prompt for an engineer to address review feedback.
func BuildFixUpPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(fixUpPromptTemplate, ticket, prURL, prNum, prNum)
}

// ClassifyReviewOutcome scans the reviewer's transcript for approval
// or change request signals. Defaults to changes_requested to avoid
// merging unreviewed code.
func ClassifyReviewOutcome(transcript string) ReviewOutcome {
	upper := strings.ToUpper(transcript)

	approveSignals := []string{"APPROVED", "LGTM", "LOOKS GOOD"}
	changeSignals := []string{"CHANGES_REQUESTED", "REQUEST CHANGES", "NEEDS CHANGES", "PLEASE FIX"}

	for _, signal := range changeSignals {
		if strings.Contains(upper, signal) {
			return ReviewChangesRequested
		}
	}

	for _, signal := range approveSignals {
		if strings.Contains(upper, signal) {
			return ReviewApproved
		}
	}

	// Default to changes requested — don't silently approve.
	return ReviewChangesRequested
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

		orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole, false)
	}()
}

func (orch *Orchestrator) runReview(ctx context.Context, reviewer *agent.Agent, prURL, ticket string, workItemID int64, engineerRole agent.Role, isReReview bool) {
	prompt := BuildReviewPrompt(prURL, ticket)
	if isReReview {
		prompt = BuildReReviewPrompt(prURL, ticket)
	}

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
		archOutcome := orch.RunConversationalArchReview(ctx, prURL, ticket, engineerRole)
		if archOutcome == ReviewChangesRequested {
			orch.handleChangesRequested(ctx, prURL, ticket, workItemID, engineerRole, "Tech Lead requested architectural changes")
			return
		}

		orch.advancePipeline(ctx, workItemID, pipeline.StageApproved)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Approved %s: %s", ticket, summary),
			agent.RoleReviewer)

		orch.mergeAndComplete(ctx, prURL, ticket, workItemID, engineerRole)

	case ReviewChangesRequested:
		orch.handleChangesRequested(ctx, prURL, ticket, workItemID, engineerRole, summary)
	}
}

// MergeForTest exports mergeAndComplete for testing.
func (orch *Orchestrator) MergeForTest(ctx context.Context, prURL, ticket string, workItemID int64) {
	orch.mergeAndComplete(ctx, prURL, ticket, workItemID, "")
}

func (orch *Orchestrator) mergeAndComplete(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	prNum := ExtractPRNumber(prURL)

	mergeAgent := orch.agents[agent.RolePM]
	if mergeAgent == nil {
		return
	}

	// Verify the PR actually has GitHub approval and CI passes.
	prompt := fmt.Sprintf(`Before merging PR #%s, verify it's ready:

1. Check review status: gh pr view %s --json reviewDecision
   - If reviewDecision is not "APPROVED", stop and say "NOT_APPROVED"
2. Check CI status: gh pr checks %s
   - If any required checks are failing, stop and say "CI_FAILING"
3. If both pass, merge: gh pr merge %s --squash --delete-branch

Respond with "done", "NOT_APPROVED", or "CI_FAILING".`, prNum, prNum, prNum, prNum)

	result, err := mergeAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("merge failed for %s: %v", ticket, err)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s approved but merge failed — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	upper := strings.ToUpper(result.Transcript)
	if strings.Contains(upper, "NOT_APPROVED") {
		log.Printf("merge blocked for %s: PR not approved on GitHub, triggering re-approval", ticket)
		orch.retryApproval(ctx, prURL, ticket, workItemID, engineerRole)
		return
	}

	if strings.Contains(upper, "CI_FAILING") {
		log.Printf("merge blocked for %s: CI failing", ticket)
		engineerName := orch.NameForRole(engineerRole)
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("%s is approved but CI is failing — %s, can you check?", ticket, engineerName),
			agent.RolePM)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageMerged)
	go MoveTicketState(ctx, mergeAgent, ticket, "Done")

	ticketLink := orch.cfg.Links.TicketLink(ticket)
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.announceAsRole(ctx, "feed",
		fmt.Sprintf("Merged %s — %s", ticketLink, prLink),
		agent.RolePM)
}

func (orch *Orchestrator) retryApproval(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		return
	}

	prNum := ExtractPRNumber(prURL)
	reviewerName := orch.NameForRole(agent.RoleReviewer)
	engineerName := orch.NameForRole(engineerRole)

	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s is approved but the GitHub review wasn't submitted — %s, re-submitting now. %s, hang tight.",
			ticket, reviewerName, engineerName),
		agent.RolePM)

	prompt := fmt.Sprintf("Your review of PR #%s was approved but the GitHub reviewDecision is not set. "+
		"Run this command now to fix it:\n\n"+
		"gh pr review %s --approve --body \"Approved\"\n\n"+
		"Then verify: gh pr view %s --json reviewDecision\n\n"+
		"Respond with just 'done' or 'failed'.", prNum, prNum, prNum)

	result, err := reviewer.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("retry approval failed for %s: %v", ticket, err)
		return
	}

	if strings.Contains(strings.ToUpper(result.Transcript), "DONE") {
		orch.mergeAfterRetry(ctx, prURL, ticket, workItemID, engineerRole)
		return
	}

	log.Printf("retry approval did not succeed for %s: %s", ticket, result.Transcript)
}

// mergeAfterRetry merges the PR without re-checking approval status.
// Called after retryApproval has already fixed the GitHub review.
func (orch *Orchestrator) mergeAfterRetry(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	prNum := ExtractPRNumber(prURL)

	mergeAgent := orch.agents[agent.RolePM]
	if mergeAgent == nil {
		return
	}

	prompt := fmt.Sprintf("Merge this PR: gh pr merge %s --squash --delete-branch\nRespond with just 'done' or 'failed'.", prNum)

	_, err := mergeAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("merge after retry failed for %s: %v", ticket, err)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageMerged)
	go MoveTicketState(ctx, mergeAgent, ticket, "Done")

	ticketLink := orch.cfg.Links.TicketLink(ticket)
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.announceAsRole(ctx, "feed",
		fmt.Sprintf("Merged %s — %s", ticketLink, prLink),
		agent.RolePM)

	_ = engineerRole
}

// RunArchitectureReviewForTest exports runArchitectureReview for testing.
func (orch *Orchestrator) RunArchitectureReviewForTest(ctx context.Context, prURL, ticket string) ReviewOutcome {
	return orch.RunConversationalArchReview(ctx, prURL, ticket, "")
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

	// Re-review: reviewer specifically checks their previous comments.
	orch.startReReview(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) startReReview(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent for re-review")
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageReviewing)
	orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole, true)
}
