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

// RunPMDuties performs the PM's active pipeline management tasks:
// checks for stale work items and follows up, verifies board state.
// Called once per tick after assignment.
func (orch *Orchestrator) RunPMDuties(ctx context.Context) {
	orch.checkStaleWork(ctx)
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
		if item.Stage != pipeline.StageWorking {
			continue
		}

		age := time.Since(item.UpdatedAt)
		if age <= staleWorkThreshold {
			continue
		}

		name := orch.NameForRole(role)
		ticketLink := orch.cfg.Links.TicketLink(item.Ticket)
		orch.announceAsRole(ctx, "engineering",
			fmt.Sprintf("Hey %s, how's %s going? It's been %s with no PR. Any blockers?",
				name, ticketLink, age.Round(time.Minute)),
			agent.RolePM)

		log.Printf("pm: followed up on stale work item %s (%s, age: %s)", item.Ticket, role, age)
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
		"Read the discussion and make the call. Be decisive — pick the approach " +
		"that makes the most sense and explain why in 2-3 sentences.\n\n"

	for _, line := range lines {
		prompt += "> " + line + "\n"
	}

	prompt += "\nMake the decision. Start with \"Decision:\" then explain briefly."

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
	return decision
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
