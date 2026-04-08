package orchestrator_test

import (
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestContainsDeferralSignal_MatchesCommonPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want bool
	}{
		{"JAM-20 stays deferred until the index spec lands", true},
		{"skip JAM-20 for now", true},
		{"hold on JAM-20", true},
		{"don't assign JAM-20 yet", true},
		{"Stop. JAM-20 stays deferred.", true},
		{"JAM-20 is ready to go", false},
		{"assign JAM-20 to Callum", false},
		{"", false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, orchestrator.ContainsDeferralSignalForTest(tt.text), tt.text)
	}
}

func TestSmartAssigner_IsDeferred_ExpiresAfterDuration(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)

	// Defer with zero duration — should expire immediately.
	sa.DeferTicket("JAM-99", 0)
	assert.False(t, sa.IsDeferred("JAM-99"))
}

func TestAssigner_DeferTicket_NoSmartAssigner_DoesNotPanic(t *testing.T) {
	t.Parallel()

	a := orchestrator.NewAssigner(nil, "")
	// No smart assigner set — should be a no-op.
	assert.NotPanics(t, func() {
		a.DeferTicket("JAM-1", time.Hour)
	})
}

func TestAssigner_DeferTicket_WithSmartAssigner_Defers(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	a := orchestrator.NewAssigner(nil, "")
	a.SetSmartAssigner(sa)

	a.DeferTicket("JAM-50", time.Hour)
	assert.True(t, sa.IsDeferred("JAM-50"))
}

func TestExtractDeferrals_DeferSignal_DefersTicket(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	a := orchestrator.NewAssigner(nil, "")
	a.SetSmartAssigner(sa)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, nil, nil, nil, a,
	)

	situations := []orchestrator.Situation{
		{Ticket: "JAM-20", Description: "JAM-20 is stale"},
		{Ticket: "JAM-21", Description: "JAM-21 needs work"},
	}

	orch.ExtractDeferralsForTest(situations,
		"JAM-20 stays deferred until the index spec is locked down by end of day.\n\n"+
			"JAM-21 is ready for implementation — assign it to whichever engineer is free.")

	assert.True(t, sa.IsDeferred("JAM-20"))
	assert.False(t, sa.IsDeferred("JAM-21"))
}

func TestExtractDeferrals_NoDeferSignal_NothingDeferred(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	a := orchestrator.NewAssigner(nil, "")
	a.SetSmartAssigner(sa)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, nil, nil, nil, a,
	)

	situations := []orchestrator.Situation{
		{Ticket: "JAM-30", Description: "JAM-30 needs work"},
	}

	orch.ExtractDeferralsForTest(situations, "JAM-30 is ready, assign to Callum.")

	assert.False(t, sa.IsDeferred("JAM-30"))
}

func TestExtractDeferrals_NilAssigner_DoesNotPanic(t *testing.T) {
	t.Parallel()

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, nil, nil, nil, nil,
	)

	situations := []orchestrator.Situation{
		{Ticket: "JAM-40", Description: "stale"},
	}

	assert.NotPanics(t, func() {
		orch.ExtractDeferralsForTest(situations, "defer JAM-40")
	})
}

func TestTicketMentionedNearDeferral_ProximityMatters(t *testing.T) {
	t.Parallel()

	// Ticket near deferral signal.
	assert.True(t, orchestrator.TicketMentionedNearDeferralForTest(
		"JAM-20 stays deferred — Kael's locking the index spec", "JAM-20"))

	// Ticket far from deferral signal — only 100 char window.
	farText := "JAM-20 is fine. " + string(make([]byte, 200)) + " defer JAM-99 instead"
	assert.False(t, orchestrator.TicketMentionedNearDeferralForTest(farText, "JAM-20"))

	// Ticket not mentioned at all.
	assert.False(t, orchestrator.TicketMentionedNearDeferralForTest(
		"defer JAM-99 please", "JAM-20"))
}
