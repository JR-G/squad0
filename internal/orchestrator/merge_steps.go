package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	ghcli "github.com/JR-G/squad0/internal/integrations/github/cli"
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

// verifyMerged checks the actual GitHub PR state via the gh CLI
// adapter directly. Earlier this method delegated to a Claude
// session and matched the substring "MERGED" in the transcript —
// brittle: the transcript could include the word in error context
// ("the PR is no longer in MERGED state") or omit it entirely
// when Claude wraps the answer in prose. The brittleness produced
// the JAM-24 failure mode where a successfully merged PR was
// processed as merge-failed and ground through review cycles
// until the orchestrator gave up.
//
// The verifyAgent param is kept for API compatibility; pass nil.
// Override via SetMergeVerifierForTest.
func (orch *Orchestrator) verifyMerged(ctx context.Context, _ *agent.Agent, prURL string) bool {
	return mergeVerifier(ctx, orch.cfg.TargetRepoDir, prURL)
}

// mergeVerifier is the package-level hook for verifying a PR is
// merged on GitHub. Production binds it to the gh CLI adapter;
// tests override via SetMergeVerifierForTest.
var mergeVerifier = func(ctx context.Context, repoDir, prURL string) bool {
	if repoDir == "" || prURL == "" {
		return false
	}
	state, err := ghcli.NewClient(repoDir).State(ctx, prURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(state.State, "MERGED")
}

// SetMergeVerifierForTest replaces the merge-verification hook with
// a test-supplied function. Returns a restore function the test
// should defer.
func SetMergeVerifierForTest(fn func(ctx context.Context, repoDir, prURL string) bool) func() {
	prev := mergeVerifier
	mergeVerifier = fn
	return func() { mergeVerifier = prev }
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

// mergeAfterRetry verifies re-approval landed and then hands the
// merge to the engineer. Since approval was just re-submitted, we
// skip the approval check inside pmFallbackMerge by attempting the
// engineer path first. If the engineer is unavailable, the PM
// executes the merge directly (approval already verified).
func (orch *Orchestrator) mergeAfterRetry(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	pmAgent := orch.agents[agent.RolePM]
	if pmAgent == nil {
		return
	}

	approvalStatus := orch.checkApprovalStatus(ctx, pmAgent, prURL)
	if approvalStatus != approvalStatusApproved {
		log.Printf("merge blocked for %s after retry: still not approved (%s)", ticket, approvalStatus)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s re-approval attempted but still not approved — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	// Engineer available — let them merge.
	if _, ok := orch.agents[engineerRole]; ok {
		orch.startEngineerMerge(ctx, prURL, ticket, workItemID, engineerRole)
		return
	}

	// No engineer — PM executes directly (approval already verified above).
	if !orch.executeMerge(ctx, pmAgent, prURL, ticket, engineerRole) {
		return
	}

	if !orch.verifyMerged(ctx, pmAgent, prURL) {
		log.Printf("merge verification failed for %s after retry", ticket)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s merge attempted after retry but PR is still open — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageMerged)
	go MoveTicketState(ctx, pmAgent, ticket, "Done")

	if !orch.hasMergeAnnounced(ticket) {
		orch.markMergeAnnounced(ticket)
		ticketLink := orch.cfg.Links.TicketLink(ticket)
		prLink := orch.cfg.Links.PRLink(prURL)
		orch.announceAsRole(ctx, "feed",
			fmt.Sprintf("Merged %s — %s", ticketLink, prLink),
			agent.RolePM)
	}
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

// hasMergeAnnounced returns true if a merge announcement has already
// been posted for the given ticket.
func (orch *Orchestrator) hasMergeAnnounced(ticket string) bool {
	orch.mergeAnnounceMu.Lock()
	defer orch.mergeAnnounceMu.Unlock()
	return orch.mergeAnnounced[ticket]
}

// markMergeAnnounced records that the merge announcement for this
// ticket has been sent, preventing duplicate posts.
func (orch *Orchestrator) markMergeAnnounced(ticket string) {
	orch.mergeAnnounceMu.Lock()
	defer orch.mergeAnnounceMu.Unlock()
	orch.mergeAnnounced[ticket] = true
}

// HasMergeAnnouncedForTest exports hasMergeAnnounced for testing.
func (orch *Orchestrator) HasMergeAnnouncedForTest(ticket string) bool {
	return orch.hasMergeAnnounced(ticket)
}

// PRHasConflictsForTest exports prHasConflicts for testing.
func (orch *Orchestrator) PRHasConflictsForTest(ctx context.Context, prURL string) bool {
	return orch.prHasConflicts(ctx, prURL)
}

// prHasConflicts checks if a PR has merge conflicts via gh CLI.
func (orch *Orchestrator) prHasConflicts(ctx context.Context, prURL string) bool {
	pmAgent := orch.agents[agent.RolePM]
	if pmAgent == nil {
		return false
	}

	prompt := fmt.Sprintf("Run: gh pr view %s --json mergeable --jq .mergeable — respond with ONLY the output", prURL)
	result, err := pmAgent.DirectSession(ctx, prompt)
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToUpper(result.Transcript), "CONFLICTING")
}

// RebaseAndMergeForTest exports rebaseAndMerge for testing.
func (orch *Orchestrator) RebaseAndMergeForTest(ctx context.Context, engineerAgent *agent.Agent, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.rebaseAndMerge(ctx, engineerAgent, prURL, ticket, workItemID, engineerRole)
}

// rebaseAndMerge handles a conflicting PR by giving the engineer a full
// worktree session to rebase onto main, resolve conflicts, push, and merge.
func (orch *Orchestrator) rebaseAndMerge(ctx context.Context, engineerAgent *agent.Agent, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	branch := fmt.Sprintf("feat/%s", strings.ToLower(ticket))
	engineerName := orch.NameForRole(engineerRole)

	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("PR for %s has conflicts — rebasing now.", orch.cfg.Links.TicketLink(ticket)),
		engineerRole)

	workSession, err := NewWorkSession(ctx, orch.cfg.TargetRepoDir, engineerRole, ticket)
	if err != nil {
		log.Printf("worktree creation failed for rebase of %s: %v", ticket, err)
		return
	}
	defer workSession.Cleanup(ctx)

	prompt := fmt.Sprintf(
		"The PR for %s has merge conflicts and cannot be merged.\n\n"+
			"PR: %s\nBranch: %s\n\n"+
			"Fix the conflicts:\n"+
			"1. git fetch origin main\n"+
			"2. git rebase origin/main\n"+
			"3. Resolve any conflicts\n"+
			"4. git push --force-with-lease origin %s\n"+
			"5. gh pr merge %s --squash --delete-branch\n\n"+
			"If the rebase is too complex, respond with FAILED.",
		ticket, prURL, branch, branch, prURL)

	_, taskErr := engineerAgent.ExecuteTask(ctx, prompt, nil, workSession.Dir())
	if taskErr != nil {
		log.Printf("rebase session failed for %s: %v", ticket, taskErr)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s has conflicts — %s, please resolve manually", ticket, engineerName),
			agent.RolePM)
		return
	}

	orch.verifyAndAnnounce(ctx, prURL, ticket, workItemID, engineerRole)
}
