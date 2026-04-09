package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/pipeline"
)

const maxReviewCycles = 3

var prURLPattern = regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/pull/\d+`)

var prRepoPattern = regexp.MustCompile(`https://github\.com/([^/]+/[^/]+)/pull/\d+`)

// ReviewOutcome classifies what the reviewer decided.
type ReviewOutcome string

const (
	// ReviewApproved means the PR passed review.
	ReviewApproved ReviewOutcome = "approved"
	// ReviewChangesRequested means the reviewer wants fixes.
	ReviewChangesRequested ReviewOutcome = "changes_requested"
)

// Approval status constants returned by checkApprovalStatus.
const (
	approvalStatusApproved    = "APPROVED"
	approvalStatusNotApproved = "NOT_APPROVED"
	approvalStatusError       = "ERROR"
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

// ExtractRepo returns "owner/repo" from a GitHub PR URL.
// For example, "https://github.com/JR-G/makebook/pull/11" returns "JR-G/makebook".
func ExtractRepo(prURL string) string {
	matches := prRepoPattern.FindStringSubmatch(prURL)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
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

		// If runReview returned without triggering the merge or fix-up
		// lifecycle (e.g. review session failed), the engineer is still
		// checked in as working. Set them idle so they're not stuck.
		if engineerRole != "" {
			orch.setIdleIfStillWorking(ctx, engineerRole)
		}
	}()
}

func (orch *Orchestrator) runReview(ctx context.Context, reviewer *agent.Agent, prURL, ticket string, workItemID int64, engineerRole agent.Role, isReReview bool) {
	log.Printf("review: starting review of %s (re-review=%v)", ticket, isReReview)

	// Narrate — team sees the reviewer is active.
	prLink := orch.cfg.Links.PRLink(prURL)
	reviewMsg := fmt.Sprintf("Picking up %s for review. %s", ticket, prLink)
	if isReReview {
		reviewMsg = fmt.Sprintf("Re-reviewing %s — checking if the feedback was addressed. %s", ticket, prLink)
	}
	orch.postAsRole(ctx, "reviews", reviewMsg, agent.RoleReviewer)

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

	switch outcome {
	case ReviewApproved:
		orch.forceApproval(ctx, reviewer, prURL, ticket)
		orch.advancePipeline(ctx, workItemID, pipeline.StageApproved)

		engineerName := orch.NameForRole(engineerRole)
		prLink := orch.cfg.Links.PRLink(prURL)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Approved %s — %s", ticket, prLink),
			agent.RoleReviewer)
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("%s, your PR is approved. Address any remaining comments and merge when ready. %s", engineerName, prLink),
			agent.RolePM)

		// Architecture review runs in the background — it does not block the merge.
		go orch.archReviewWithTimeout(ctx, prURL, ticket, engineerRole)

		// Merge is SYNCHRONOUS — the review goroutine owns the full
		// lifecycle: review → approve → merge. No async event dispatch
		// here because that caused races (merge starting while a
		// parallel review was still running).
		orch.emitEvent(ctx, EventPRApproved, prURL, ticket, workItemID, engineerRole)
		orch.startEngineerMerge(ctx, prURL, ticket, workItemID, engineerRole)

	case ReviewChangesRequested:
		orch.emitEvent(ctx, EventChangesRequested, prURL, ticket, workItemID, engineerRole)
		orch.handleChangesRequested(ctx, prURL, ticket, workItemID, engineerRole, "")
	}
}

// StartEngineerMergeForTest exports startEngineerMerge for testing.
func (orch *Orchestrator) StartEngineerMergeForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.startEngineerMerge(ctx, prURL, ticket, workItemID, engineerRole)
}

// MergeForTest exports startEngineerMerge for backwards-compatible testing.
func (orch *Orchestrator) MergeForTest(ctx context.Context, prURL, ticket string, workItemID int64) {
	orch.startEngineerMerge(ctx, prURL, ticket, workItemID, "")
}

// MergeWithEngineerForTest exports startEngineerMerge with engineer role for testing.
func (orch *Orchestrator) MergeWithEngineerForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.startEngineerMerge(ctx, prURL, ticket, workItemID, engineerRole)
}

// startEngineerMerge gives the engineer ownership of merging their own
// approved PR. Checks mergeability first — if conflicting, runs a full
// worktree session to rebase. Otherwise uses DirectSession for the merge.
func (orch *Orchestrator) startEngineerMerge(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	engineerAgent, ok := orch.agents[engineerRole]
	if !ok {
		log.Printf("no agent for %s to merge PR — falling back to PM", engineerRole)
		orch.pmFallbackMerge(ctx, prURL, ticket, workItemID, engineerRole)
		return
	}

	_ = orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         engineerRole,
		Ticket:        ticket,
		Status:        coordination.StatusWorking,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("merging %s", ticket),
	})
	defer func() { _ = orch.checkIns.SetIdle(ctx, engineerRole) }()

	// Check mergeability — if conflicting, rebase in a worktree first.
	if orch.prHasConflicts(ctx, prURL) {
		log.Printf("merge: %s has conflicts — rebasing in worktree", ticket)
		orch.rebaseAndMerge(ctx, engineerAgent, prURL, ticket, workItemID, engineerRole)
		return
	}

	prompt := BuildEngineerMergePrompt(prURL, ticket)
	result, err := engineerAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("engineer merge session failed for %s: %v", ticket, err)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Merge session failed for %s — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	_ = result
	orch.verifyAndAnnounce(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) verifyAndAnnounce(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	pmAgent := orch.agents[agent.RolePM]
	verifyAgent := orch.agents[engineerRole]
	if pmAgent != nil {
		verifyAgent = pmAgent
	}

	if verifyAgent == nil {
		return
	}

	if !orch.verifyMerged(ctx, verifyAgent, prURL) {
		log.Printf("engineer merge failed for %s: PR not merged after session", ticket)
		engineerName := orch.NameForRole(engineerRole)
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("%s merge failed — %s, can you check and retry?", ticket, engineerName),
			agent.RolePM)
		orch.emitEvent(ctx, EventMergeFailed, prURL, ticket, workItemID, engineerRole)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageMerged)
	if pmAgent != nil {
		go MoveTicketState(ctx, pmAgent, ticket, "Done")
	}

	if !orch.hasMergeAnnounced(ticket) {
		orch.markMergeAnnounced(ticket)
		ticketLink := orch.cfg.Links.TicketLink(ticket)
		prLink := orch.cfg.Links.PRLink(prURL)
		orch.announceAsRole(ctx, "feed",
			fmt.Sprintf("Merged %s — %s", ticketLink, prLink),
			engineerRole)
	}
	orch.emitEvent(ctx, EventMergeComplete, prURL, ticket, workItemID, engineerRole)
}

// pmFallbackMerge is used when the engineer agent is not available.
// The PM runs the merge steps directly as a last resort.
func (orch *Orchestrator) pmFallbackMerge(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	pmAgent := orch.agents[agent.RolePM]
	if pmAgent == nil {
		return
	}

	// Verify approval is registered on GitHub before attempting merge.
	approvalStatus := orch.checkApprovalStatus(ctx, pmAgent, prURL)
	if approvalStatus == approvalStatusNotApproved {
		orch.retryApproval(ctx, prURL, ticket, workItemID, engineerRole)
		return
	}

	if approvalStatus == approvalStatusError {
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s approved but could not verify approval — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	if !orch.executeMerge(ctx, pmAgent, prURL, ticket, engineerRole) {
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s approved but merge failed — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	if !orch.verifyMerged(ctx, pmAgent, prURL) {
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s merge attempted but PR is still open — needs manual merge", ticket),
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

const archReviewTimeout = 2 * time.Minute

// archReviewWithTimeout runs the architecture review with a deadline.
// If the Tech Lead doesn't respond within 2 minutes, the review is
// skipped and the merge proceeds. A stuck Opus session should never
// block the pipeline indefinitely.
func (orch *Orchestrator) archReviewWithTimeout(ctx context.Context, prURL, ticket string, engineerRole agent.Role) ReviewOutcome {
	resultCh := make(chan ReviewOutcome, 1)

	archCtx, cancel := context.WithTimeout(ctx, archReviewTimeout)
	defer cancel()

	go func() {
		resultCh <- orch.RunConversationalArchReview(archCtx, prURL, ticket, engineerRole)
	}()

	select {
	case outcome := <-resultCh:
		return outcome
	case <-archCtx.Done():
		log.Printf("arch review timed out for %s after %s — proceeding with merge", ticket, archReviewTimeout)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Architecture review for %s timed out — proceeding with merge. Sable, please review post-merge.", ticket),
			agent.RolePM)
		return ReviewApproved
	}
}

// RunArchitectureReviewForTest exports runArchitectureReview for testing.
func (orch *Orchestrator) RunArchitectureReviewForTest(ctx context.Context, prURL, ticket string) ReviewOutcome {
	return orch.RunConversationalArchReview(ctx, prURL, ticket, "")
}

func (orch *Orchestrator) handleChangesRequested(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role, _ string) {
	orch.advancePipeline(ctx, workItemID, pipeline.StageChangesRequested)

	if orch.shouldEscalate(ctx, workItemID, ticket) {
		// Escalated — free the engineer so they can pick up other work.
		_ = orch.checkIns.SetIdle(ctx, engineerRole)
		return
	}

	// Extract structured review comments before fix-up.
	comments := fetchStructuredComments(ctx, orch.cfg.TargetRepoDir, prURL)

	engineerName := orch.NameForRole(engineerRole)
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s, changes requested on %s — review comments are on the PR. %s", engineerName, ticket, prLink),
		agent.RoleReviewer)

	orch.startFixUp(ctx, prURL, ticket, workItemID, engineerRole, comments)
}

func (orch *Orchestrator) startFixUp(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role, comments []ReviewComment) {
	engineerAgent, ok := orch.agents[engineerRole]
	if !ok {
		log.Printf("no agent for %s to address review feedback", engineerRole)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageWorking)

	_ = orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         engineerRole,
		Ticket:        ticket,
		Status:        coordination.StatusWorking,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("fixing up %s", ticket),
	})
	defer func() { _ = orch.checkIns.SetIdle(ctx, engineerRole) }()

	engineerName := orch.NameForRole(engineerRole)
	log.Printf("fix-up: %s starting fix-up session for %s (%s)", engineerName, ticket, prURL)

	// Narrate start — triggers conversation engine so team can react.
	ticketLink := orch.cfg.Links.TicketLink(ticket)
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Picking up the review feedback on %s — reading through the comments now.", ticketLink),
		engineerRole)

	// Brief pause so the conversation engine processes replies,
	// then the engineer acknowledges before going heads-down.
	time.Sleep(orch.acknowledgePause())
	orch.acknowledgeThread(ctx, engineerAgent, engineerRole, "engineering")

	// Work in a worktree on the PR branch so the agent doesn't
	// create a new branch/PR. If worktree creation fails, fall back
	// to TargetRepoDir so fix-ups still attempt to run.
	workDir, fixUpSession := orch.setupFixUpWorktree(ctx, prURL, engineerRole, ticket)
	if fixUpSession != nil {
		defer fixUpSession.Cleanup(ctx)
	}

	orch.writeMCPConfig(engineerAgent, workDir)
	defer func() { _ = agent.RemoveMCPConfig(workDir) }()

	handoffCtx := BuildHandoffContext(ctx, orch.handoffStore, ticket)
	checklist := FormatFixUpChecklist(comments)
	prompt := handoffCtx + BuildFixUpPrompt(prURL, ticket) + checklist
	branch := fmt.Sprintf("feat/%s", strings.ToLower(ticket))

	result, err := engineerAgent.ExecuteTask(ctx, prompt, nil, workDir)
	if err != nil {
		log.Printf("fix-up session failed for %s on %s: %v", engineerRole, ticket, err)
		orch.writeHandoff(ctx, ticket, engineerRole, "failed", result.Transcript, branch)
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Hit a snag fixing up %s — will need another pass.", ticketLink),
			engineerRole)
		return
	}

	orch.writeHandoff(ctx, ticket, engineerRole, "completed", result.Transcript, branch)

	RunPreSubmitCheck(ctx, engineerAgent, workDir)

	// Verify which review comments were addressed in the diff.
	verifiedComments := comments
	if len(comments) > 0 {
		diffCmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "origin/main...HEAD")
		diffCmd.Dir = workDir
		diffOut, _ := diffCmd.Output()
		verifiedComments = CheckCommentsAddressedWithDiff(comments, string(diffOut))
	}

	log.Printf("fix-up: %s completed fix-up for %s", engineerName, ticket)

	prLink := orch.cfg.Links.PRLink(prURL)
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Addressed the review comments on %s — %s. Pushed and ready for re-review. %s",
			ticketLink, prLink, SummariseVerification(verifiedComments)),
		engineerRole)

	orch.emitEvent(ctx, EventFixUpComplete, prURL, ticket, workItemID, engineerRole)
	orch.startReReview(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) setupFixUpWorktree(ctx context.Context, prURL string, role agent.Role, ticket string) (string, *WorkSession) {
	if ctx.Err() != nil {
		return orch.cfg.TargetRepoDir, nil
	}
	session, err := NewFixUpSession(ctx, orch.cfg.TargetRepoDir, prURL, role, ticket)
	if err != nil {
		log.Printf("fix-up: worktree failed for %s, using repo dir: %v", ticket, err)
		return orch.cfg.TargetRepoDir, nil
	}
	dir := session.Dir()
	return dir, session
}

// forceApproval ensures the reviewer's GitHub approval is actually submitted.
func (orch *Orchestrator) startReReview(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent for re-review")
		return
	}

	log.Printf("re-review: starting re-review of %s for %s", ticket, prURL)
	orch.advancePipeline(ctx, workItemID, pipeline.StageReviewing)
	orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole, true)
	log.Printf("re-review: completed re-review of %s", ticket)
}
