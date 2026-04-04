package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestSituationQueue_Push_AddsSituation(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	queue.Push(orchestrator.Situation{
		Type:     orchestrator.SitUnmergedApprovedPR,
		Severity: orchestrator.SeverityInfo,
		Engineer: agent.RoleEngineer1,
		Ticket:   "JAM-1",
	})

	assert.Equal(t, 1, queue.Len())
}

func TestSituationQueue_Push_Deduplicates(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{
		Type:     orchestrator.SitUnmergedApprovedPR,
		Severity: orchestrator.SeverityInfo,
		Engineer: agent.RoleEngineer1,
		Ticket:   "JAM-1",
	}

	queue.Push(sit)
	queue.Push(sit) // Same key — should be deduped.

	assert.Equal(t, 1, queue.Len())
}

func TestSituationQueue_Drain_ReturnsAllAndClears(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	queue.Push(orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-1",
	})
	queue.Push(orchestrator.Situation{
		Type: orchestrator.SitOrphanedPR, Ticket: "JAM-2",
	})

	items := queue.Drain()
	assert.Len(t, items, 2)
	assert.Equal(t, 0, queue.Len())
}

func TestSituationQueue_Drain_Empty_ReturnsNil(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	assert.Nil(t, queue.Drain())
}

func TestSituationQueue_Resolve_PreventsPushCooldown(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{
		Type: orchestrator.SitUnmergedApprovedPR, Ticket: "JAM-1",
	}

	queue.Push(sit)
	queue.Drain()
	queue.Resolve(sit.Key())

	// Push again — should be blocked by cooldown.
	queue.Push(sit)
	assert.Equal(t, 0, queue.Len())
}

func TestSituationQueue_Escalate_BumpsSeverityAndRequeues(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{
		Type:     orchestrator.SitStaleWorkingAgent,
		Severity: orchestrator.SeverityInfo,
		Ticket:   "JAM-1",
	}

	queue.Escalate(sit)

	items := queue.Drain()
	assert.Len(t, items, 1)
	assert.Equal(t, orchestrator.SeverityWarning, items[0].Severity)
	assert.Equal(t, 1, items[0].Escalations)
}

func TestSituationQueue_Escalate_WarningToCritical(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{
		Type:     orchestrator.SitRepeatedFailure,
		Severity: orchestrator.SeverityWarning,
		Ticket:   "JAM-2",
	}

	queue.Escalate(sit)

	items := queue.Drain()
	assert.Len(t, items, 1)
	assert.Equal(t, orchestrator.SeverityCritical, items[0].Severity)
}

func TestSituation_Key_IsUnique(t *testing.T) {
	t.Parallel()

	sitA := orchestrator.Situation{
		Type: orchestrator.SitUnmergedApprovedPR, Ticket: "JAM-1", Engineer: agent.RoleEngineer1,
	}
	sitB := orchestrator.Situation{
		Type: orchestrator.SitUnmergedApprovedPR, Ticket: "JAM-2", Engineer: agent.RoleEngineer1,
	}

	assert.NotEqual(t, sitA.Key(), sitB.Key())
}

func TestFormatForPM_GroupsBySeverity(t *testing.T) {
	t.Parallel()

	situations := []orchestrator.Situation{
		{Type: orchestrator.SitStaleWorkingAgent, Severity: orchestrator.SeverityInfo, Description: "info item"},
		{Type: orchestrator.SitRepeatedFailure, Severity: orchestrator.SeverityCritical, Description: "critical item"},
		{Type: orchestrator.SitPipelineDrift, Severity: orchestrator.SeverityWarning, Description: "warning item"},
	}

	prompt := orchestrator.FormatForPM(situations)

	// Critical should appear before warning, warning before info.
	critIdx := len(prompt) // fallback
	warnIdx := len(prompt)
	infoIdx := len(prompt)

	for idx := range prompt {
		remaining := prompt[idx:]
		if critIdx == len(prompt) && len(remaining) > 8 && remaining[:8] == "CRITICAL" {
			critIdx = idx
		}
		if warnIdx == len(prompt) && len(remaining) > 7 && remaining[:7] == "WARNING" {
			warnIdx = idx
		}
		if infoIdx == len(prompt) && len(remaining) > 4 && remaining[:4] == "INFO" {
			infoIdx = idx
		}
	}

	assert.Less(t, critIdx, warnIdx, "critical should appear before warning")
	assert.Less(t, warnIdx, infoIdx, "warning should appear before info")
}

func TestFormatForPM_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", orchestrator.FormatForPM(nil))
}
