package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptForPhase_AllPhases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phase   orchestrator.ThreadPhase
		state   orchestrator.ThreadState
		wantSub string
	}{
		{"exploring", orchestrator.PhaseExploring, orchestrator.ThreadState{}, "Share your perspective"},
		{"debating_with_points", orchestrator.PhaseDebating, orchestrator.ThreadState{KeyPoints: []string{"REST", "GraphQL"}}, "Points raised"},
		{"debating_no_points", orchestrator.PhaseDebating, orchestrator.ThreadState{}, "weighing options"},
		{"converging", orchestrator.PhaseConverging, orchestrator.ThreadState{}, "aligning"},
		{"decided_with_decision", orchestrator.PhaseDecided, orchestrator.ThreadState{Decision: "Use REST"}, "Use REST"},
		{"decided_no_decision", orchestrator.PhaseDecided, orchestrator.ThreadState{}, "decision was reached"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, orchestrator.PromptForPhase(tt.phase, tt.state), tt.wantSub)
		})
	}
}

func TestPromptForPhase_ManyPointsTruncated(t *testing.T) {
	t.Parallel()
	state := orchestrator.ThreadState{KeyPoints: []string{"A", "B", "C", "D", "E", "F"}}
	assert.Contains(t, orchestrator.PromptForPhase(orchestrator.PhaseDebating, state), "C")
}

func TestSummariseThread_WithQuestion(t *testing.T) {
	t.Parallel()
	lines := make([]string, 12)
	for i := range 12 {
		lines[i] = "agent: msg"
	}
	lines[11] = "ceo: what?"
	assert.Contains(t, orchestrator.SummariseThread(lines, 5), "unanswered")
}

func TestSummariseThread_NoQuestion(t *testing.T) {
	t.Parallel()
	lines := make([]string, 12)
	for i := range 12 {
		lines[i] = "agent: msg"
	}
	assert.NotContains(t, orchestrator.SummariseThread(lines, 5), "unanswered")
}

func TestContainsProjectSignal_Matches(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsProjectSignalForTest("The module boundaries"))
	assert.False(t, orchestrator.ContainsProjectSignalForTest("plain message"))
}

func TestPersistFindings_NoKeywords_SkipsPM(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, _ := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	t.Cleanup(func() { _ = sqlDB.Close() })
	ci := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, ci.InitSchema(ctx))
	pr := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pm := setupPMAgent(t, pr)
	o := orchestrator.NewOrchestrator(orchestrator.Config{}, map[agent.Role]*agent.Agent{agent.RolePM: pm}, ci, nil, nil)
	o.PersistFindings(ctx, "JAM-NF", "everything fine")
	pr.mu.Lock()
	assert.Equal(t, 0, len(pr.calls))
	pr.mu.Unlock()
}

func TestPersistFindings_Discovery_InvokesPM(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, _ := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	t.Cleanup(func() { _ = sqlDB.Close() })
	ci := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, ci.InitSchema(ctx))
	pr := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pm := setupPMAgent(t, pr)
	o := orchestrator.NewOrchestrator(orchestrator.Config{}, map[agent.Role]*agent.Agent{agent.RolePM: pm}, ci, nil, nil)
	o.PersistFindings(ctx, "JAM-FD", "I discovered hidden rate limits. This was unexpected.")
	pr.mu.Lock()
	assert.Greater(t, len(pr.calls), 0)
	pr.mu.Unlock()
}

func TestContainsFindings_Table(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsFindings("discovered a bug"))
	assert.False(t, orchestrator.ContainsFindings("fine"))
}

func TestBuildSeanceContext_Nil_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.BuildSeanceContext(context.Background(), nil, "J1", agent.RoleEngineer1))
}

func TestBuildSeanceContextFull_Episodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memDB, _ := memory.Open(ctx, ":memory:")
	t.Cleanup(func() { _ = memDB.Close() })
	es := memory.NewEpisodeStore(memDB)
	_, _ = es.CreateEpisode(ctx, memory.Episode{
		Agent: string(agent.RoleEngineer2), Ticket: "JAM-SE", Summary: "Fixed API", Outcome: memory.OutcomePartial,
	})
	assert.Contains(t, orchestrator.BuildSeanceContext(ctx, es, "JAM-SE", agent.RoleEngineer1), "Fixed API")
}

func TestBuildSeanceContextFull_Handoffs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memDB, _ := memory.Open(ctx, ":memory:")
	t.Cleanup(func() { _ = memDB.Close() })
	sqlDB, _ := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	t.Cleanup(func() { _ = sqlDB.Close() })
	ho := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, ho.InitSchema(ctx))
	_, _ = ho.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-HS", Agent: string(agent.RoleEngineer2), Status: "failed", Summary: "Tests failing", Blockers: "CI",
	})
	es := memory.NewEpisodeStore(memDB)
	assert.Contains(t, orchestrator.BuildSeanceContextFull(ctx, es, nil, ho, "JAM-HS", agent.RoleEngineer1), "Tests failing")
}

func TestEscalation_Ack_Prevents(t *testing.T) {
	t.Parallel()
	tr := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-AK", Description: "stale", Severity: orchestrator.SeverityWarning}
	tr.Track(sit)
	tr.Acknowledge(sit.Key())
	tr.BackdateForTest(sit.Key(), 5*time.Hour)
	assert.Empty(t, tr.CheckStale())
}

func TestEscalation_AutoBlocked(t *testing.T) {
	t.Parallel()
	tr := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-BK", Description: "stuck", Severity: orchestrator.SeverityCritical, Escalations: 2}
	tr.Track(sit)
	tr.BackdateForTest(sit.Key(), 5*time.Hour)
	assert.Len(t, tr.AutoBlocked(), 1)
}

func TestWriteHandoff_NilStore_NoPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, _ := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	t.Cleanup(func() { _ = sqlDB.Close() })
	ci := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, ci.InitSchema(ctx))
	o := orchestrator.NewOrchestrator(orchestrator.Config{}, map[agent.Role]*agent.Agent{}, ci, nil, nil)
	assert.NotPanics(t, func() { o.WriteHandoffForTest(ctx, "J", agent.RoleEngineer1, "f", "s", "b") })
}

func TestWriteHandoff_FailedSetsDirty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, _ := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	t.Cleanup(func() { _ = sqlDB.Close() })
	ci := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, ci.InitSchema(ctx))
	ho := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, ho.InitSchema(ctx))
	o := orchestrator.NewOrchestrator(orchestrator.Config{}, map[agent.Role]*agent.Agent{}, ci, nil, nil)
	o.SetHandoffStore(ho)
	o.WriteHandoffForTest(ctx, "JAM-HF2", agent.RoleEngineer1, "failed", "broke", "feat/hf2")
	h, _ := ho.LatestForTicket(ctx, "JAM-HF2")
	assert.Equal(t, "dirty", h.GitState)
}

func TestFilterIdleDutyRoles_PassthroughIncludesPM(t *testing.T) {
	t.Parallel()
	result := orchestrator.FilterIdleDutyRolesForTest(
		[]agent.Role{agent.RolePM, agent.RoleReviewer, agent.RoleEngineer1, agent.RoleTechLead})
	assert.Contains(t, result, agent.RolePM)
	assert.Contains(t, result, agent.RoleReviewer)
	assert.Len(t, result, 4)
}
