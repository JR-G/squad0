package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestThreadTracker_NewThread_IsExploring(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	state := tracker.Get("engineering")

	assert.Equal(t, orchestrator.PhaseExploring, state.Phase)
	assert.Equal(t, 0, state.TurnCount)
}

func TestThreadTracker_Update_IncrementsTurnCount(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "here's my plan for the auth module")
	tracker.Update("engineering", agent.RoleTechLead, "looks reasonable")

	state := tracker.Get("engineering")
	assert.Equal(t, 2, state.TurnCount)
	assert.Equal(t, 2, len(state.Participants))
}

func TestThreadTracker_DecisionSignal_MovesToDecided(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "I think we should use approach A")
	tracker.Update("engineering", agent.RoleTechLead, "DECISION: use approach A with the adapter pattern")

	state := tracker.Get("engineering")
	assert.Equal(t, orchestrator.PhaseDecided, state.Phase)
	assert.Equal(t, "use approach A with the adapter pattern", state.Decision)
}

func TestThreadTracker_DebateSignals_MovesToDebating(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "I think we should use a queue here")
	tracker.Update("engineering", agent.RoleEngineer2, "alternatively, we could use a simple channel instead")

	state := tracker.Get("engineering")
	assert.Equal(t, orchestrator.PhaseDebating, state.Phase)
}

func TestThreadTracker_ConvergenceSignals_MovesToConverging(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "I think we should use approach A")
	tracker.Update("engineering", agent.RoleTechLead, "that could work, let me think")
	tracker.Update("engineering", agent.RoleEngineer2, "agreed, that makes sense to me")

	state := tracker.Get("engineering")
	assert.Equal(t, orchestrator.PhaseConverging, state.Phase)
}

func TestThreadTracker_Reset_ClearsState(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "some message")
	tracker.Reset("engineering")

	state := tracker.Get("engineering")
	assert.Equal(t, orchestrator.PhaseExploring, state.Phase)
	assert.Equal(t, 0, state.TurnCount)
}

func TestThreadTracker_MultipleChannels_Independent(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "DECISION: use approach A")
	tracker.Update("reviews", agent.RoleReviewer, "reviewing now")

	eng := tracker.Get("engineering")
	rev := tracker.Get("reviews")

	assert.Equal(t, orchestrator.PhaseDecided, eng.Phase)
	assert.Equal(t, orchestrator.PhaseExploring, rev.Phase)
}

func TestThreadTracker_KeyPoints_ExtractsAndDeduplicates(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer1, "The auth module needs to support OAuth tokens.")
	tracker.Update("engineering", agent.RoleEngineer2, "The auth module needs to support OAuth tokens.")

	state := tracker.Get("engineering")
	// Should deduplicate identical points.
	assert.Equal(t, 1, len(state.KeyPoints))
}

func TestThreadTracker_ShortMessages_NoKeyPoints(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("engineering", agent.RoleEngineer3, "interesting")
	tracker.Update("engineering", agent.RoleEngineer3, "hm")

	state := tracker.Get("engineering")
	assert.Equal(t, 0, len(state.KeyPoints))
}

func TestConversationEngine_ThreadTrackerForTest_ReturnsTracker(t *testing.T) {
	t.Parallel()

	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)
	tracker := engine.ThreadTrackerForTest()
	assert.NotNil(t, tracker)
}

func TestPromptForPhase_Exploring_IsOpen(t *testing.T) {
	t.Parallel()

	state := orchestrator.ThreadState{Phase: orchestrator.PhaseExploring}
	prompt := orchestrator.PromptForPhase(orchestrator.PhaseExploring, state)

	assert.Contains(t, prompt, "Share your perspective")
}

func TestPromptForPhase_Debating_EncouragesNewAngles(t *testing.T) {
	t.Parallel()

	state := orchestrator.ThreadState{
		Phase:     orchestrator.PhaseDebating,
		KeyPoints: []string{"use a queue", "use a channel"},
	}
	prompt := orchestrator.PromptForPhase(orchestrator.PhaseDebating, state)

	assert.Contains(t, prompt, "new")
	assert.Contains(t, prompt, "don't respond")
	assert.Contains(t, prompt, "use a queue")
}

func TestPromptForPhase_Converging_HighBar(t *testing.T) {
	t.Parallel()

	state := orchestrator.ThreadState{Phase: orchestrator.PhaseConverging}
	prompt := orchestrator.PromptForPhase(orchestrator.PhaseConverging, state)

	assert.Contains(t, prompt, "aligning")
	assert.Contains(t, prompt, "specific problem")
}

func TestPromptForPhase_Decided_IncludesDecision(t *testing.T) {
	t.Parallel()

	state := orchestrator.ThreadState{
		Phase:    orchestrator.PhaseDecided,
		Decision: "use approach A",
	}
	prompt := orchestrator.PromptForPhase(orchestrator.PhaseDecided, state)

	assert.Contains(t, prompt, "use approach A")
	assert.Contains(t, prompt, "critical issue")
}

func TestAdjustForPhase_Exploring_NoChange(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2, orchestrator.AdjustForPhaseForTest(2, orchestrator.PhaseExploring, false))
}

func TestAdjustForPhase_Debating_CapsAtOne(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, orchestrator.AdjustForPhaseForTest(2, orchestrator.PhaseDebating, false))
	assert.Equal(t, 1, orchestrator.AdjustForPhaseForTest(1, orchestrator.PhaseDebating, false))
}

func TestAdjustForPhase_Converging_AgentSilenced(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, orchestrator.AdjustForPhaseForTest(2, orchestrator.PhaseConverging, false))
	assert.Equal(t, 1, orchestrator.AdjustForPhaseForTest(2, orchestrator.PhaseConverging, true))
}

func TestAdjustForPhase_Decided_OnlyHumans(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, orchestrator.AdjustForPhaseForTest(2, orchestrator.PhaseDecided, false))
	assert.Equal(t, 1, orchestrator.AdjustForPhaseForTest(2, orchestrator.PhaseDecided, true))
}

func TestExtractDecision_WithPrefix(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("eng", agent.RoleTechLead, "After thinking about it:\nDECISION: use the adapter pattern\nThat settles it.")

	state := tracker.Get("eng")
	assert.Equal(t, orchestrator.PhaseDecided, state.Phase)
	assert.Equal(t, "use the adapter pattern", state.Decision)
}

func TestExtractDecision_LowerCase(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("eng", agent.RoleTechLead, "decision: go with approach B")

	state := tracker.Get("eng")
	assert.Equal(t, orchestrator.PhaseDecided, state.Phase)
}

func TestDecidedPhase_StaysDecided(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("eng", agent.RolePM, "DECISION: ship it")
	tracker.Update("eng", agent.RoleEngineer2, "I'm not convinced — what about testing?")

	state := tracker.Get("eng")
	// Once decided, debate signals shouldn't revert the phase.
	assert.Equal(t, orchestrator.PhaseDecided, state.Phase)
}

func TestImplicitDebate_ManyTurns_ManyParticipants(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewThreadTracker()
	tracker.Update("eng", agent.RoleEngineer1, "here's a thought about the design")
	tracker.Update("eng", agent.RoleEngineer2, "yeah that could work")
	tracker.Update("eng", agent.RoleEngineer1, "what about edge case X though")
	tracker.Update("eng", agent.RoleTechLead, "good point about edge case X")

	state := tracker.Get("eng")
	// 4 turns with 3 participants but no explicit debate signal — should
	// still move past exploring.
	assert.NotEqual(t, orchestrator.PhaseExploring, state.Phase)
}
