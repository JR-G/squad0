package orchestrator_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmartAssigner_CircuitBreaker(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)

	assert.False(t, sa.IsCircuitOpen("JAM-1"))
	sa.RecordFailure("JAM-1")
	sa.RecordFailure("JAM-1")
	assert.False(t, sa.IsCircuitOpen("JAM-1"))
	sa.RecordFailure("JAM-1")
	assert.True(t, sa.IsCircuitOpen("JAM-1"))
}

func TestSmartAssigner_FilterAndRank_SkipsCircuitOpen(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	sa.RecordFailure("JAM-BAD")
	sa.RecordFailure("JAM-BAD")
	sa.RecordFailure("JAM-BAD")

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-BAD", Title: "broken", Priority: 1},
		{ID: "JAM-OK", Title: "fine", Priority: 2},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1})
	require.Len(t, assignments, 1)
	assert.Equal(t, "JAM-OK", assignments[0].Ticket)
}

func TestSmartAssigner_FilterAndRank_SkipsUnmetDeps(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-2", Title: "depends", Priority: 2, DependsOn: []string{"JAM-1"}},
		{ID: "JAM-3", Title: "no deps", Priority: 3},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1})
	require.Len(t, assignments, 1)
	assert.Equal(t, "JAM-3", assignments[0].Ticket)
}

func TestSmartAssigner_FilterAndRank_PriorityOrder(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-LOW", Title: "low", Priority: 4},
		{ID: "JAM-URG", Title: "urgent", Priority: 1},
		{ID: "JAM-MED", Title: "medium", Priority: 3},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{
		agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3,
	})

	require.Len(t, assignments, 3)
	assert.Equal(t, "JAM-URG", assignments[0].Ticket)
	assert.Equal(t, "JAM-MED", assignments[1].Ticket)
	assert.Equal(t, "JAM-LOW", assignments[2].Ticket)
}

func TestSmartAssigner_FilterAndRank_SkillMatching(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-API", Title: "API work", Labels: []string{"API"}, Priority: 2},
		{ID: "JAM-UI", Title: "Frontend work", Labels: []string{"Frontend"}, Priority: 2},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{
		agent.RoleEngineer1, agent.RoleEngineer2,
	})

	require.Len(t, assignments, 2)
	// Engineer-1 (backend) should get API, Engineer-2 (frontend) should get Frontend.
	assert.Equal(t, agent.RoleEngineer1, assignments[0].Role)
	assert.Equal(t, "JAM-API", assignments[0].Ticket)
	assert.Equal(t, agent.RoleEngineer2, assignments[1].Role)
	assert.Equal(t, "JAM-UI", assignments[1].Ticket)
}

func TestSmartAssigner_FilterAndRank_SkipsInPipeline(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	_, _ = store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-WIP", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	sa := orchestrator.NewSmartAssigner(store)

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-WIP", Title: "already working", Priority: 1},
		{ID: "JAM-NEW", Title: "new ticket", Priority: 2},
	}

	assignments := sa.FilterAndRank(ctx, tickets, []agent.Role{agent.RoleEngineer2})
	require.Len(t, assignments, 1)
	assert.Equal(t, "JAM-NEW", assignments[0].Ticket)
}

func TestSmartAssigner_FilterAndRank_SkipsFailedWithOpenPR(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	// Simulate: engineer went idle, item marked failed, but PR is still open.
	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-PR", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/42")
	_ = store.Advance(ctx, itemID, pipeline.StageFailed)

	sa := orchestrator.NewSmartAssigner(store)

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-PR", Title: "has open PR", Priority: 1},
		{ID: "JAM-FREE", Title: "no work yet", Priority: 2},
	}

	assignments := sa.FilterAndRank(ctx, tickets, []agent.Role{agent.RoleEngineer2})
	require.Len(t, assignments, 1)
	assert.Equal(t, "JAM-FREE", assignments[0].Ticket)
}

func TestSmartAssigner_FailureCount(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	assert.Equal(t, 0, sa.FailureCount("JAM-1"))
	sa.RecordFailure("JAM-1")
	assert.Equal(t, 1, sa.FailureCount("JAM-1"))
}

func TestSmartAssigner_DeferTicket_SkipsDeferredTickets(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	sa.DeferTicket("JAM-20", 1*time.Hour)

	assert.True(t, sa.IsDeferred("JAM-20"))
	assert.False(t, sa.IsDeferred("JAM-21"))

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-20", Title: "deferred", Priority: 1},
		{ID: "JAM-21", Title: "available", Priority: 2},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1})
	require.Len(t, assignments, 1)
	assert.Equal(t, "JAM-21", assignments[0].Ticket)
}

func TestAssigner_SetLinearAPIKey_EnablesSmartDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, memDB)

	assigner := orchestrator.NewAssigner(pmAgent, "TEAM-1")
	assigner.SetLinearAPIKey("test-key")
	assigner.SetSmartAssigner(orchestrator.NewSmartAssigner(nil))

	// smartAssign will fail (no real Linear API) but shouldn't panic.
	// It falls back to PM assign.
	assignments, assignErr := assigner.RequestAssignments(ctx, []agent.Role{agent.RoleEngineer1})
	_ = assignments
	_ = assignErr
}

func TestAssigner_RecordAssignmentFailure_OpensCircuitAt3(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, memDB)

	assigner := orchestrator.NewAssigner(pmAgent, "TEAM-1")
	assigner.SetSmartAssigner(orchestrator.NewSmartAssigner(nil))

	assert.False(t, assigner.RecordAssignmentFailure("JAM-1"))
	assert.False(t, assigner.RecordAssignmentFailure("JAM-1"))
	assert.True(t, assigner.RecordAssignmentFailure("JAM-1"))
}

func TestAssigner_RecordAssignmentFailure_NilSmartAssigner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, memDB)

	assigner := orchestrator.NewAssigner(pmAgent, "TEAM-1")
	assert.False(t, assigner.RecordAssignmentFailure("JAM-1"))
}

func TestAssigner_SmartAssign_EndToEnd(t *testing.T) {
	t.Parallel()

	linearResponse := `{"data":{"team":{"issues":{"nodes":[
		{"identifier":"JAM-10","title":"API ticket","description":"Build the API","priority":2,"state":{"name":"Todo"},"labels":{"nodes":[{"name":"API"}]}},
		{"identifier":"JAM-11","title":"UI ticket","description":"Build the UI","priority":3,"state":{"name":"Todo"},"labels":{"nodes":[{"name":"Frontend"}]}}
	]}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(linearResponse))
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, memDB)

	assigner := orchestrator.NewAssigner(pmAgent, "TEAM-1")
	assigner.SetLinearAPIKey("test-key")
	assigner.SetLinearURL(server.URL)
	assigner.SetSmartAssigner(orchestrator.NewSmartAssigner(nil))

	assignments, assignErr := assigner.RequestAssignments(ctx, []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2})
	require.NoError(t, assignErr)
	require.Len(t, assignments, 2)

	// Engineer-1 (backend) should get JAM-10 (API), Engineer-2 (frontend) should get JAM-11 (Frontend).
	assert.Equal(t, "JAM-10", assignments[0].Ticket)
	assert.Equal(t, agent.RoleEngineer1, assignments[0].Role)
	assert.Equal(t, "JAM-11", assignments[1].Ticket)
	assert.Equal(t, agent.RoleEngineer2, assignments[1].Role)
}

func TestSmartAssigner_FilterAndRank_DepsMetViaCompletedTickets(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	// Mark JAM-7 as done.
	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-7", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.Advance(ctx, itemID, pipeline.StageMerged))

	sa := orchestrator.NewSmartAssigner(store)

	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-12", Title: "depends on 7", Priority: 2, DependsOn: []string{"JAM-7"}},
		{ID: "JAM-13", Title: "depends on 99", Priority: 2, DependsOn: []string{"JAM-99"}},
	}

	assignments := sa.FilterAndRank(ctx, tickets, []agent.Role{agent.RoleEngineer1})
	require.Len(t, assignments, 1)
	assert.Equal(t, "JAM-12", assignments[0].Ticket)
}

func TestPriorityRank_ZeroPriorityIsLast(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-NONE", Title: "none", Priority: 0},
		{ID: "JAM-LOW", Title: "low", Priority: 4},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2})
	require.Len(t, assignments, 2)
	assert.Equal(t, "JAM-LOW", assignments[0].Ticket)
	assert.Equal(t, "JAM-NONE", assignments[1].Ticket)
}

func TestTruncateDescription_ShortString(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-1", Title: "test", Description: "short", Priority: 2},
	}

	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1})
	require.Len(t, assignments, 1)
	assert.Contains(t, assignments[0].Description, "short")
}

func TestParseDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"simple", "**Depends on:** JAM-7", []string{"JAM-7"}},
		{"multiple", "Depends on: JAM-7, JAM-8", []string{"JAM-7", "JAM-8"}},
		{"mixed case", "depends on JAM-12", []string{"JAM-12"}},
		{"no deps", "This ticket has no dependencies", nil},
		{"embedded", "Some text\n**Depends on:** JAM-9 (types)\nMore text", []string{"JAM-9"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.ParseDependenciesForTest(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
