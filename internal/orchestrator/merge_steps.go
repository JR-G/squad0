package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// checkApprovalStatus runs a single-purpose prompt to verify the PR's
// GitHub review decision. Returns approvalStatusApproved, approvalStatusNotApproved, or approvalStatusError.
func (orch *Orchestrator) checkApprovalStatus(ctx context.Context, mergeAgent *agent.Agent, prURL string) string {
	prompt := fmt.Sprintf("Run: gh pr view %s --json reviewDecision --jq .reviewDecision — respond with ONLY the output", prURL)

	result, err := mergeAgent.DirectSession(ctx, prompt)
	if err != nil {
		return approvalStatusError
	}

	upper := strings.ToUpper(result.Transcript)

	rejectionSignals := []string{"NOT_APPROVED", "REVIEW_REQUIRED", "CHANGES_REQUESTED", "PENDING"}
	for _, signal := range rejectionSignals {
		if strings.Contains(upper, signal) {
			return approvalStatusNotApproved
		}
	}

	if strings.Contains(upper, "APPROVED") {
		return approvalStatusApproved
	}

	return approvalStatusNotApproved
}

// CheckApprovalStatusForTest exports checkApprovalStatus for testing.
func (orch *Orchestrator) CheckApprovalStatusForTest(ctx context.Context, mergeAgent *agent.Agent, prURL string) string {
	return orch.checkApprovalStatus(ctx, mergeAgent, prURL)
}

// executeMerge runs a single-purpose prompt to squash-merge the PR.
func (orch *Orchestrator) executeMerge(ctx context.Context, mergeAgent *agent.Agent, prURL, ticket string, engineerRole agent.Role) bool {
	prompt := fmt.Sprintf("Run: gh pr merge %s --squash --delete-branch — respond with ONLY 'done' or the error message", prURL)

	result, err := mergeAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("merge failed for %s: %v", ticket, err)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s approved but merge failed — needs manual merge", ticket),
			agent.RolePM)
		return false
	}

	upper := strings.ToUpper(result.Transcript)
	if strings.Contains(upper, "CI") && strings.Contains(upper, "FAIL") {
		log.Printf("merge blocked for %s: CI failing", ticket)
		engineerName := orch.NameForRole(engineerRole)
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("%s is approved but CI is failing — %s, can you fix and push?", ticket, engineerName),
			agent.RolePM)
		return false
	}

	return true
}

// ExecuteMergeForTest exports executeMerge for testing.
func (orch *Orchestrator) ExecuteMergeForTest(ctx context.Context, mergeAgent *agent.Agent, prURL, ticket string, engineerRole agent.Role) bool {
	return orch.executeMerge(ctx, mergeAgent, prURL, ticket, engineerRole)
}

// VerifyMergedForTest exports verifyMerged for testing.
func (orch *Orchestrator) VerifyMergedForTest(ctx context.Context, verifyAgent *agent.Agent, prURL string) bool {
	return orch.verifyMerged(ctx, verifyAgent, prURL)
}

// verifyMerged checks the actual GitHub PR state to confirm it was merged.
func (orch *Orchestrator) verifyMerged(ctx context.Context, verifyAgent *agent.Agent, prURL string) bool {
	prompt := fmt.Sprintf("Run this command and respond with ONLY the output, nothing else:\ngh pr view %s --json state --jq .state", prURL)

	result, err := verifyAgent.DirectSession(ctx, prompt)
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToUpper(result.Transcript), "MERGED")
}

func (orch *Orchestrator) retryApproval(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		return
	}

	reviewerName := orch.NameForRole(agent.RoleReviewer)
	engineerName := orch.NameForRole(engineerRole)

	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s is approved but the GitHub review wasn't submitted — %s, re-submitting now. %s, hang tight.",
			ticket, reviewerName, engineerName),
		agent.RolePM)

	approvePrompt := fmt.Sprintf("Run: gh pr review %s --approve --body 'Approved' — respond with ONLY 'done' or the error", prURL)

	_, err := reviewer.DirectSession(ctx, approvePrompt)
	if err != nil {
		log.Printf("retry approval failed for %s: %v", ticket, err)
		return
	}

	verifyPrompt := fmt.Sprintf("Run this and respond with ONLY the output:\ngh pr view %s --json reviewDecision --jq .reviewDecision", prURL)

	verifyResult, verifyErr := reviewer.DirectSession(ctx, verifyPrompt)
	if verifyErr != nil {
		log.Printf("approval verification failed for %s: %v", ticket, verifyErr)
		return
	}

	if !strings.Contains(strings.ToUpper(verifyResult.Transcript), "APPROVED") {
		log.Printf("retry approval did not land for %s: %s", ticket, verifyResult.Transcript)
		return
	}

	log.Printf("retry approval verified for %s, retrying merge", ticket)
	orch.mergeAfterRetry(ctx, prURL, ticket, workItemID, engineerRole)
}

// MergeAfterRetryForTest exports mergeAfterRetry for testing.
func (orch *Orchestrator) MergeAfterRetryForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.mergeAfterRetry(ctx, prURL, ticket, workItemID, engineerRole)
}

// mergeAfterRetry is mergeAndComplete without the retryApproval fallback.
func (orch *Orchestrator) mergeAfterRetry(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	mergeAgent := orch.agents[agent.RolePM]
	if mergeAgent == nil {
		return
	}

	approvalStatus := orch.checkApprovalStatus(ctx, mergeAgent, prURL)
	if approvalStatus != approvalStatusApproved {
		log.Printf("merge blocked for %s after retry: still not approved (%s)", ticket, approvalStatus)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s re-approval attempted but still not approved — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	if !orch.executeMerge(ctx, mergeAgent, prURL, ticket, engineerRole) {
		return
	}

	if !orch.verifyMerged(ctx, mergeAgent, prURL) {
		log.Printf("merge verification failed for %s after retry", ticket)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s merge attempted after retry but PR is still open — needs manual merge", ticket),
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

// forceApproval ensures the GitHub review is actually submitted.
func (orch *Orchestrator) forceApproval(ctx context.Context, reviewer *agent.Agent, prURL, ticket string) {
	prompt := fmt.Sprintf("Run this command now:\ngh pr review %s --approve --body \"Approved\"\nRespond with ONLY 'done' or the error.", prURL)

	_, err := reviewer.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("force approval failed for %s: %v", ticket, err)
	}

	log.Printf("review: forced approval submission for %s", ticket)
}

// ForceApprovalForTest exports forceApproval for testing.
func (orch *Orchestrator) ForceApprovalForTest(ctx context.Context, reviewer *agent.Agent, prURL, ticket string) {
	orch.forceApproval(ctx, reviewer, prURL, ticket)
}
