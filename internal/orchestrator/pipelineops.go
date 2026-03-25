package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/pipeline"
)

// SetPipelinePRForTest exports setPipelinePR for testing.
func (orch *Orchestrator) SetPipelinePRForTest(ctx context.Context, itemID int64, prURL string) {
	orch.setPipelinePR(ctx, itemID, prURL)
}

// StoreProjectEpisodeForTest exports storeProjectEpisode for testing.
func (orch *Orchestrator) StoreProjectEpisodeForTest(ctx context.Context, role agent.Role, ticket, transcript string) {
	orch.storeProjectEpisode(ctx, role, ticket, transcript)
}

// CreatePipelineItemForTest exports createPipelineItem for testing.
func (orch *Orchestrator) CreatePipelineItemForTest(ctx context.Context, assignment Assignment) int64 {
	return orch.createPipelineItem(ctx, assignment)
}

func (orch *Orchestrator) setPipelinePR(ctx context.Context, itemID int64, prURL string) {
	if orch.pipelineStore == nil || itemID == 0 {
		return
	}

	_ = orch.pipelineStore.SetPRURL(ctx, itemID, prURL)
	orch.advancePipeline(ctx, itemID, pipeline.StagePROpened)
}

func (orch *Orchestrator) storeProjectEpisode(ctx context.Context, role agent.Role, ticket, transcript string) {
	if orch.projectEpisodeStore == nil {
		return
	}

	_, _ = orch.projectEpisodeStore.CreateEpisode(ctx, memory.Episode{
		Agent:   string(role),
		Ticket:  ticket,
		Summary: agent.TruncateSummary(transcript, 500),
		Outcome: memory.OutcomeSuccess,
	})
}

func (orch *Orchestrator) createPipelineItem(ctx context.Context, assignment Assignment) int64 {
	if orch.pipelineStore == nil {
		return 0
	}

	branch := fmt.Sprintf("feat/%s", strings.ToLower(assignment.Ticket))
	itemID, err := orch.pipelineStore.Create(ctx, pipeline.WorkItem{
		Ticket:   assignment.Ticket,
		Engineer: assignment.Role,
		Stage:    pipeline.StageWorking,
		Branch:   branch,
	})
	if err != nil {
		log.Printf("failed to create pipeline item for %s: %v", assignment.Ticket, err)
		return 0
	}

	return itemID
}

func (orch *Orchestrator) advancePipeline(ctx context.Context, itemID int64, stage pipeline.Stage) {
	if orch.pipelineStore == nil || itemID == 0 {
		return
	}

	if err := orch.pipelineStore.Advance(ctx, itemID, stage); err != nil {
		log.Printf("failed to advance pipeline item %d to %s: %v", itemID, stage, err)
	}
}

func (orch *Orchestrator) shouldEscalate(ctx context.Context, workItemID int64, ticket string) bool {
	if orch.pipelineStore == nil || workItemID == 0 {
		return false
	}

	_ = orch.pipelineStore.IncrementReviewCycles(ctx, workItemID)

	item, err := orch.pipelineStore.GetByID(ctx, workItemID)
	if err != nil {
		return false
	}

	if item.ReviewCycles < maxReviewCycles {
		return false
	}

	orch.announceAsRole(ctx, "triage",
		fmt.Sprintf("%s has had %d review cycles — needs human attention", ticket, item.ReviewCycles),
		agent.RolePM)

	return true
}

func (orch *Orchestrator) filterByWIP(ctx context.Context, roles []agent.Role) []agent.Role {
	if orch.pipelineStore == nil {
		return roles
	}

	available := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		openItems, err := orch.pipelineStore.OpenByEngineer(ctx, role)
		if err != nil {
			available = append(available, role)
			continue
		}
		if len(openItems) > 0 {
			log.Printf("tick: skipping %s — has %d open work items", role, len(openItems))
			continue
		}
		available = append(available, role)
	}

	return available
}
