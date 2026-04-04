package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/pipeline"
)

const (
	// sensorApprovedStale is how long an approved PR can sit before
	// the sensor flags it. Shorter than the PM's nudge threshold
	// because the PM will decide whether to act.
	sensorApprovedStale = 10 * time.Minute

	// sensorWorkingStale is how long an engineer can be "working"
	// with no PR before the sensor flags it.
	sensorWorkingStale = 45 * time.Minute

	// maxRepeatedFailures is how many times the same ticket can
	// fail before the sensor flags it as a pattern.
	maxRepeatedFailures = 2
)

// RunSensorsForTest exports RunSensors for testing.
func (orch *Orchestrator) RunSensorsForTest(t interface{ Helper() }) {
	t.Helper()
	orch.RunSensors(context.Background())
}

// CheckIns returns the check-in store for testing.
func (orch *Orchestrator) CheckIns() *coordination.CheckInStore {
	return orch.checkIns
}

// SetSituationQueue connects the situation queue for PM management.
func (orch *Orchestrator) SetSituationQueue(queue *SituationQueue) {
	orch.situations = queue
}

// SetEscalationTracker connects the escalation tracker for stale
// triage detection and auto-blocking.
func (orch *Orchestrator) SetEscalationTracker(tracker *EscalationTracker) {
	orch.escalations = tracker
}

// RunSensors executes all sensors and pushes detected situations
// into the queue. Called every tick — must be cheap (no Claude sessions).
func (orch *Orchestrator) RunSensors(ctx context.Context) {
	if orch.situations == nil {
		return
	}

	orch.senseUnmergedApproved(ctx)
	orch.senseStaleWorking(ctx)
	orch.sensePipelineDrift(ctx)
	orch.senseRepeatedFailures(ctx)
	orch.RunEscalationCheck(ctx)
}

// senseUnmergedApproved detects approved PRs sitting unmerged.
func (orch *Orchestrator) senseUnmergedApproved(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	engineers := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}
	for _, role := range engineers {
		items, err := orch.pipelineStore.OpenByEngineer(ctx, role)
		if err != nil {
			continue
		}

		for _, item := range items {
			if item.Stage != pipeline.StageApproved {
				continue
			}
			if time.Since(item.UpdatedAt) <= sensorApprovedStale {
				continue
			}

			name := orch.NameForRole(role)
			orch.situations.Push(Situation{
				Type:        SitUnmergedApprovedPR,
				Severity:    SeverityInfo,
				Engineer:    role,
				Ticket:      item.Ticket,
				PRURL:       item.PRURL,
				Description: fmt.Sprintf("%s has an approved PR for %s sitting unmerged for %s", name, item.Ticket, formatDuration(time.Since(item.UpdatedAt))),
			})
		}
	}
}

// senseStaleWorking detects engineers who've been "working" too long
// with no PR. Something might be stuck.
func (orch *Orchestrator) senseStaleWorking(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	engineers := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}
	for _, role := range engineers {
		items, err := orch.pipelineStore.OpenByEngineer(ctx, role)
		if err != nil {
			continue
		}

		for _, item := range items {
			if item.Stage != pipeline.StageWorking || item.PRURL != "" {
				continue
			}
			if time.Since(item.UpdatedAt) <= sensorWorkingStale {
				continue
			}

			name := orch.NameForRole(role)
			orch.situations.Push(Situation{
				Type:        SitStaleWorkingAgent,
				Severity:    SeverityWarning,
				Engineer:    role,
				Ticket:      item.Ticket,
				Description: fmt.Sprintf("%s has been working on %s for %s with no PR", name, item.Ticket, formatDuration(time.Since(item.UpdatedAt))),
			})
		}
	}
}

// sensePipelineDrift detects mismatches between check-in state and
// pipeline state. An engineer checked in as "idle" but with open
// pipeline items is a drift signal.
func (orch *Orchestrator) sensePipelineDrift(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	engineers := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}
	for _, role := range engineers {
		checkIn, err := orch.checkIns.GetByAgent(ctx, role)
		if err != nil {
			continue
		}

		if checkIn.Status != coordination.StatusIdle {
			continue
		}

		items, openErr := orch.pipelineStore.OpenByEngineer(ctx, role)
		if openErr != nil || len(items) == 0 {
			continue
		}

		// Idle but has open items — that's drift.
		for _, item := range items {
			if item.PRURL == "" {
				continue
			}
			name := orch.NameForRole(role)
			orch.situations.Push(Situation{
				Type:        SitPipelineDrift,
				Severity:    SeverityWarning,
				Engineer:    role,
				Ticket:      item.Ticket,
				PRURL:       item.PRURL,
				Description: fmt.Sprintf("%s is idle but has an open PR for %s (stage: %s)", name, item.Ticket, item.Stage),
			})
		}
	}
}

// senseRepeatedFailures detects tickets that keep failing across attempts.
func (orch *Orchestrator) senseRepeatedFailures(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	failures := orch.pipelineStore.RecentFailures(ctx, 24*time.Hour)
	counts := make(map[string]int)

	for _, item := range failures {
		counts[item.Ticket]++
	}

	for ticket, count := range counts {
		if count <= maxRepeatedFailures {
			continue
		}

		severity := SeverityWarning
		if count >= 4 {
			severity = SeverityCritical
		}

		orch.situations.Push(Situation{
			Type:        SitRepeatedFailure,
			Severity:    severity,
			Ticket:      ticket,
			Description: fmt.Sprintf("%s has failed %d times — the ticket may need rewriting or investigation", ticket, count),
		})
	}
}
