package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

const staleWorkThreshold = 30 * time.Minute

// staleApprovedThreshold is how long an approved PR can sit before
// the PM nudges the engineer to merge.
const staleApprovedThreshold = 15 * time.Minute

// RunPMDuties performs the PM's active pipeline management tasks:
// drains the situation queue (sensors detect, PM decides), checks
// for stale work items, and verifies board state.
func (orch *Orchestrator) RunPMDuties(ctx context.Context) {
	orch.processSituations(ctx)
	orch.checkStaleWork(ctx)
}

// processSituations drains the situation queue and lets the PM make
// judgment calls in a single batched session. Token-efficient: one
// Claude session handles all pending situations.
func (orch *Orchestrator) processSituations(ctx context.Context) {
	if orch.situations == nil {
		return
	}

	situations := orch.situations.Drain()
	if len(situations) == 0 {
		return
	}

	pmAgent, ok := orch.agents[agent.RolePM]
	if !ok {
		return
	}

	log.Printf("pm: processing %d situations", len(situations))

	prompt := FormatForPM(situations)
	response, err := pmAgent.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("pm: situation processing failed: %v", err)
		return
	}

	// Post the PM's actions to the appropriate channels.
	orch.dispatchPMActions(ctx, situations, response)
}

// dispatchPMActions posts the PM's response and handles escalation.
// Critical situations also get flagged in triage. Extracts deferral
// signals so the assigner skips tickets the PM wants held.
func (orch *Orchestrator) dispatchPMActions(ctx context.Context, situations []Situation, response string) {
	response = filterPassResponse(response)
	if response == "" {
		return
	}

	// Extract deferrals before posting — the assigner needs to know
	// before the next tick's tryAssignWork runs.
	orch.extractDeferrals(situations, response)

	// Post the PM's management actions to engineering.
	orch.postAsRole(ctx, "engineering", response, agent.RolePM)

	// Flag warning/critical situations in triage and track for staleness.
	for _, sit := range situations {
		orch.escalateSituation(ctx, sit)
		orch.situations.Resolve(sit.Key())
	}
}

// extractDeferrals scans the PM's response for deferral signals and
// marks mentioned tickets as deferred in the assigner. Deferral lasts
// 24 hours — the PM can re-evaluate next cycle.
func (orch *Orchestrator) extractDeferrals(situations []Situation, response string) {
	if orch.assigner == nil {
		return
	}

	if !containsDeferralSignal(response) {
		return
	}

	// Check each situation's ticket — if the PM's response mentions
	// deferral and the ticket appears near a deferral word, hold it.
	for _, sit := range situations {
		if sit.Ticket == "" {
			continue
		}
		if ticketMentionedNearDeferral(response, sit.Ticket) {
			orch.assigner.DeferTicket(sit.Ticket, 24*time.Hour)
		}
	}
}

func (orch *Orchestrator) escalateSituation(ctx context.Context, sit Situation) {
	if sit.Severity != SeverityWarning && sit.Severity != SeverityCritical {
		return
	}

	orch.announceAsRole(ctx, "triage",
		fmt.Sprintf("[%s] %s (escalation #%d)", sit.Type, sit.Description, sit.Escalations),
		agent.RolePM)

	if orch.escalations != nil {
		orch.escalations.Track(sit)
	}
}

// ExtractDeferralsForTest exports extractDeferrals for testing.
func (orch *Orchestrator) ExtractDeferralsForTest(situations []Situation, response string) {
	orch.extractDeferrals(situations, response)
}

// PostDailySummary posts a summary of pipeline state to #feed.
// Called by the scheduler at standup time.
func (orch *Orchestrator) PostDailySummary(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	summary := orch.buildDailySummary(ctx)
	if summary == "" {
		return
	}

	orch.announceAsRole(ctx, "feed", summary, agent.RolePM)
}

func (orch *Orchestrator) buildDailySummary(ctx context.Context) string {
	engineers := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}

	var completed, inReview, blocked int
	var statsLines []string

	for _, role := range engineers {
		items, err := orch.pipelineStore.CompletedByEngineer(ctx, role)
		if err != nil {
			continue
		}

		stats := pipeline.ComputeStats(role, items)
		completed += stats.Completed
		name := orch.NameForRole(role)

		if stats.Completed > 0 || stats.Failed > 0 {
			statsLines = append(statsLines, fmt.Sprintf("- %s: %d completed, %d failed, avg %.1f review cycles",
				name, stats.Completed, stats.Failed, stats.AvgReviewCycles))
		}

		open, openErr := orch.pipelineStore.OpenByEngineer(ctx, role)
		if openErr != nil {
			continue
		}

		for _, item := range open {
			switch item.Stage { //nolint:exhaustive // only counting review/blocked stages
			case pipeline.StageReviewing, pipeline.StagePROpened:
				inReview++
			case pipeline.StageChangesRequested:
				blocked++
			}
		}
	}

	summary := fmt.Sprintf("*Daily Summary*\n"+
		"Tickets completed: %d | In review: %d | Blocked: %d",
		completed, inReview, blocked)

	if len(statsLines) > 0 {
		summary += "\n\n*Agent Performance:*\n"
		for _, line := range statsLines {
			summary += line + "\n"
		}
	}

	return summary
}

func (orch *Orchestrator) checkStaleWork(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	engineers := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}

	for _, role := range engineers {
		orch.checkStaleForEngineer(ctx, role)
	}
}

func (orch *Orchestrator) checkStaleForEngineer(ctx context.Context, role agent.Role) {
	openItems, err := orch.pipelineStore.OpenByEngineer(ctx, role)
	if err != nil {
		return
	}

	for _, item := range openItems {
		orch.nudgeStaleItem(ctx, role, item)
	}
}

func (orch *Orchestrator) nudgeStaleItem(ctx context.Context, role agent.Role, item pipeline.WorkItem) {
	if orch.followedUp[item.ID] {
		return
	}

	age := time.Since(item.UpdatedAt)
	name := orch.NameForRole(role)
	ticketLink := orch.cfg.Links.TicketLink(item.Ticket)

	switch item.Stage { //nolint:exhaustive // only nudging for specific stages
	case pipeline.StageWorking:
		if age <= staleWorkThreshold {
			return
		}
		orch.followedUp[item.ID] = true
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Hey %s, how's %s going? It's been %s with no PR. Any blockers?",
				name, ticketLink, formatDuration(age)),
			agent.RolePM)
		log.Printf("pm: followed up on stale work item %s (%s, age: %s)", item.Ticket, role, age)

	case pipeline.StageApproved:
		if age <= staleApprovedThreshold {
			return
		}
		orch.followedUp[item.ID] = true
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("Hey %s, %s is approved — ready to merge?", name, ticketLink),
			agent.RolePM)
		log.Printf("pm: nudged stale approved item %s (%s, age: %s)", item.Ticket, role, age)
	}
}

// BreakDiscussionTie has the PM make a decision when a discussion has
// been going on for too long without consensus. Returns the PM's call.
func (orch *Orchestrator) BreakDiscussionTie(ctx context.Context, channel string) string {
	pmAgent, ok := orch.agents[agent.RolePM]
	if !ok {
		return ""
	}

	lines := make([]string, 0)
	if orch.conversation != nil {
		lines = orch.conversation.RecentMessages(channel)
	}

	if len(lines) == 0 {
		return ""
	}

	prompt := "The team has been discussing an approach but hasn't reached consensus. " +
		"Make the call as PM — priorities, scope, and timeline. NOT technical decisions. " +
		"Leave architecture to the Tech Lead. Your job: what to build and what to skip. " +
		"Start with 'Decision:'. Use Slack formatting (*bold* not **bold**). Use people's names.\n\n"

	for _, line := range lines {
		prompt += "> " + line + "\n"
	}

	prompt += "\nMake the decision now."

	decision, err := pmAgent.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("pm: tie-breaking failed: %v", err)
		return ""
	}

	decision = filterPassResponse(decision)
	if decision == "" {
		return ""
	}

	orch.postAsRole(ctx, channel, decision, agent.RolePM)

	// If the PM's response contains a Decision: line, store it as an
	// architecture decision for future recall.
	decisionLine := ExtractDecisionLine(decision)
	if decisionLine != "" {
		orch.storeBreakTieDecision(ctx, decisionLine)
	}

	return decision
}

// storeBreakTieDecision persists the PM's tie-breaking decision as an
// architecture decision via the Tech Lead's fact store.
func (orch *Orchestrator) storeBreakTieDecision(ctx context.Context, decision string) {
	orch.StoreArchitectureDecision(ctx, decision, "discussion-tiebreak")
}

// FormatDurationForTest exports formatDuration for testing.
func FormatDurationForTest(d time.Duration) string {
	return formatDuration(d)
}

// formatDuration returns a human-readable duration like "2h 41m" or "45m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// VerifyTicketState checks that the Linear board matches pipeline state.
// Moves tickets that are out of sync.
func (orch *Orchestrator) VerifyTicketState(ctx context.Context, ticket, expectedState string) {
	pmAgent, ok := orch.agents[agent.RolePM]
	if !ok {
		return
	}

	go MoveTicketState(ctx, pmAgent, ticket, expectedState)
}
