package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// PRState represents the actual GitHub state of a pull request,
// fetched directly via gh CLI. No Claude session needed.
type PRState struct {
	URL            string `json:"url"`
	State          string `json:"state"`          // "OPEN", "CLOSED", "MERGED"
	ReviewDecision string `json:"reviewDecision"` // "APPROVED", "CHANGES_REQUESTED", ""
	Mergeable      string `json:"mergeable"`      // "MERGEABLE", "CONFLICTING", "UNKNOWN"
}

// PRStateFetcher returns the current GitHub state of all PRs.
type PRStateFetcher func(ctx context.Context) (map[string]PRState, error)

// ParsePRStates parses JSON from `gh pr list` into a map keyed by URL.
func ParsePRStates(data []byte) (map[string]PRState, error) {
	var prs []PRState
	if err := json.Unmarshal(data, &prs); err != nil {
		return nil, fmt.Errorf("parsing PR states: %w", err)
	}
	result := make(map[string]PRState, len(prs))
	for _, pr := range prs {
		result[pr.URL] = pr
	}
	return result, nil
}

// NewGHPRStateFetcher returns a fetcher that calls gh pr list.
func NewGHPRStateFetcher(repoDir string) PRStateFetcher {
	return func(ctx context.Context) (map[string]PRState, error) {
		cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--state", "all", "--limit", "50",
			"--json", "url,state,reviewDecision,mergeable")
		cmd.Dir = repoDir
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("gh pr list: %w", err)
		}
		return ParsePRStates(output)
	}
}

// ReconcileGitHubState syncs pipeline against actual GitHub PR state.
// One CLI call per tick, no Claude sessions.
func (orch *Orchestrator) ReconcileGitHubState(ctx context.Context) {
	if orch.pipelineStore == nil || orch.cfg.TargetRepoDir == "" {
		return
	}
	fetcher := NewGHPRStateFetcher(orch.cfg.TargetRepoDir)
	orch.reconcileWithFetcher(ctx, fetcher)
}

// ReconcileWithFetcherForTest exports reconcileWithFetcher for testing.
func (orch *Orchestrator) ReconcileWithFetcherForTest(ctx context.Context, fetch PRStateFetcher) {
	orch.reconcileWithFetcher(ctx, fetch)
}

func (orch *Orchestrator) reconcileWithFetcher(ctx context.Context, fetch PRStateFetcher) {
	ghStates, err := fetch(ctx)
	if err != nil {
		log.Printf("reconcile: failed to fetch PR states: %v", err)
		return
	}
	orch.ReconcileWithStates(ctx, ghStates)
}

// ReconcileWithStates reconciles pipeline items against the given PR
// state map. Separated from ReconcileGitHubState for testability.
func (orch *Orchestrator) ReconcileWithStates(ctx context.Context, ghStates map[string]PRState) {
	if orch.pipelineStore == nil {
		return
	}
	openItems, err := orch.pipelineStore.OpenWithPR(ctx)
	if err != nil || len(openItems) == 0 {
		return
	}
	for _, item := range openItems {
		ghState, ok := ghStates[item.PRURL]
		if !ok {
			continue
		}
		orch.reconcileItem(ctx, item, ghState)
	}
}

// ReconcileItemForTest exports reconcileItem for testing.
func (orch *Orchestrator) ReconcileItemForTest(ctx context.Context, item pipeline.WorkItem, ghState PRState) {
	orch.reconcileItem(ctx, item, ghState)
}

func (orch *Orchestrator) reconcileItem(ctx context.Context, item pipeline.WorkItem, ghState PRState) {
	switch strings.ToUpper(ghState.State) {
	case "MERGED":
		orch.reconcileMerged(ctx, item)
	case "CLOSED":
		orch.reconcileClosed(ctx, item)
	case "OPEN":
		orch.reconcileOpen(ctx, item, ghState)
	}
}

func (orch *Orchestrator) reconcileMerged(ctx context.Context, item pipeline.WorkItem) {
	if item.Stage == pipeline.StageMerged {
		return
	}
	log.Printf("reconcile: %s is merged on GitHub — advancing pipeline", item.Ticket)
	orch.advancePipeline(ctx, item.ID, pipeline.StageMerged)

	pmAgent := orch.agents[agent.RolePM]
	if pmAgent != nil {
		go MoveTicketState(ctx, pmAgent, item.Ticket, "Done")
	}
}

func (orch *Orchestrator) reconcileClosed(ctx context.Context, item pipeline.WorkItem) {
	if item.Stage == pipeline.StageFailed {
		return
	}
	log.Printf("reconcile: %s PR was closed without merge — marking failed", item.Ticket)
	orch.advancePipeline(ctx, item.ID, pipeline.StageFailed)
}

func (orch *Orchestrator) reconcileOpen(ctx context.Context, item pipeline.WorkItem, ghState PRState) {
	decision := strings.ToUpper(ghState.ReviewDecision)

	if item.Stage == pipeline.StageApproved && decision != approvalStatusApproved {
		log.Printf("reconcile: %s pipeline says approved but GitHub says %q — reverting to reviewing", item.Ticket, decision)
		orch.advancePipeline(ctx, item.ID, pipeline.StageReviewing)
		return
	}

	if orch.blockedByOutstandingComments(ctx, item, decision) {
		return
	}

	switch {
	case decision == approvalStatusApproved && item.Stage != pipeline.StageApproved:
		log.Printf("reconcile: %s is approved on GitHub — advancing", item.Ticket)
		orch.advancePipeline(ctx, item.ID, pipeline.StageApproved)

	case decision == "CHANGES_REQUESTED" && item.Stage != pipeline.StageChangesRequested:
		log.Printf("reconcile: %s has changes requested — advancing", item.Ticket)
		orch.advancePipeline(ctx, item.ID, pipeline.StageChangesRequested)

	case strings.ToUpper(ghState.Mergeable) == "CONFLICTING":
		log.Printf("reconcile: %s has conflicts", item.Ticket)
		if orch.situations != nil {
			name := orch.NameForRole(item.Engineer)
			orch.situations.Push(Situation{
				Type:        SitPipelineDrift,
				Severity:    SeverityWarning,
				Engineer:    item.Engineer,
				Ticket:      item.Ticket,
				PRURL:       item.PRURL,
				Description: fmt.Sprintf("%s's PR for %s has merge conflicts", name, item.Ticket),
			})
		}
	}
}

// blockedByOutstandingComments returns true if a PR that GitHub
// reports as APPROVED still has unaddressed structured review
// comments (e.g. from Devin or CodeRabbit). In that case the item
// is dropped back to reviewing so the fix-up loop runs instead of
// advancing to merge.
func (orch *Orchestrator) blockedByOutstandingComments(ctx context.Context, item pipeline.WorkItem, decision string) bool {
	if decision != approvalStatusApproved {
		return false
	}
	if !HasOutstandingReviewComments(ctx, orch.cfg.TargetRepoDir, item.PRURL) {
		return false
	}
	log.Printf("reconcile: %s is approved but has unaddressed review comments — staying in reviewing", item.Ticket)
	if item.Stage != pipeline.StageReviewing {
		orch.advancePipeline(ctx, item.ID, pipeline.StageReviewing)
	}
	return true
}
