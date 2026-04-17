package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// recoverOrphanedPRs — additional branch coverage
// ---------------------------------------------------------------------------

func TestRecoverOrphanedPRs_NilPipelineStore_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// No pipeline store and no target repo dir.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// Should return early without panic — pipelineStore is nil.
	assert.NotPanics(t, func() {
		orch.RecoverOrphanedPRsForTest(ctx)
	})
}

func TestRecoverOrphanedPRs_EmptyTargetRepoDir_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	// Pipeline store set but target repo dir is empty — early return.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: ""},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	assert.NotPanics(t, func() {
		orch.RecoverOrphanedPRsForTest(ctx)
	})
}

// ---------------------------------------------------------------------------
// hasPipelineItem — additional branch coverage
// ---------------------------------------------------------------------------

func TestHasPipelineItem_AllTerminal_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	// Create items that are both terminal.
	id1, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-TERM", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})
	require.NoError(t, pipeStore.Advance(ctx, id1, pipeline.StageMerged))

	id2, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-TERM", Engineer: agent.RoleEngineer2, Stage: pipeline.StageWorking,
	})
	require.NoError(t, pipeStore.Advance(ctx, id2, pipeline.StageFailed))

	// All items are terminal — should return false.
	assert.False(t, orch.HasPipelineItemForTest(ctx, "JAM-TERM"))
}

func TestHasPipelineItem_DBError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	// Close the DB to force errors.
	_ = sqlDB.Close()

	assert.False(t, orch.HasPipelineItemForTest(ctx, "JAM-ERROR"))
}

// ---------------------------------------------------------------------------
// parseOpenPRs — edge cases
// ---------------------------------------------------------------------------

func TestParseOpenPRs_MissingURL_Skipped(t *testing.T) {
	t.Parallel()

	// Entry with no URL field.
	output := `[{"headRefName":"feat/jam-1","number":1}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Empty(t, prs)
}

func TestParseOpenPRs_MissingBranch_Skipped(t *testing.T) {
	t.Parallel()

	output := `[{"number":1,"url":"https://github.com/test/pull/1"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Empty(t, prs)
}

func TestParseOpenPRs_NonFeatBranch_Skipped(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"fix/something","number":1,"url":"https://github.com/test/pull/1"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Empty(t, prs)
}

func TestParseOpenPRs_CaseInsensitiveBranch(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"Feat/JAM-42","number":1,"url":"https://github.com/test/pull/1"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	require.Len(t, prs, 1)
}

func TestParseOpenPRs_MultiplePRs_MixedValid(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"feat/jam-1","number":1,"url":"https://github.com/test/pull/1"},{"headRefName":"main","number":2,"url":"https://github.com/test/pull/2"},{"headRefName":"feat/jam-3","number":3,"url":"https://github.com/test/pull/3"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Len(t, prs, 2)
}

// ---------------------------------------------------------------------------
// extractJSONField — edge cases
// ---------------------------------------------------------------------------

func TestExtractJSONField_EmptyLine(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.ExtractJSONFieldForTest("", "headRefName"))
}

func TestExtractJSONField_UnterminatedValue(t *testing.T) {
	t.Parallel()

	// Field value has no closing quote.
	line := `{"headRefName":"feat/jam-1`
	assert.Empty(t, orchestrator.ExtractJSONFieldForTest(line, "headRefName"))
}

func TestExtractJSONField_FieldAtEnd(t *testing.T) {
	t.Parallel()

	line := `{"url":"https://github.com/test/pull/5"}`
	assert.Equal(t, "https://github.com/test/pull/5", orchestrator.ExtractJSONFieldForTest(line, "url"))
}

// ---------------------------------------------------------------------------
// guessEngineer — always returns engineer-1
// ---------------------------------------------------------------------------

func TestGuessEngineer_AnyBranch_ReturnsEngineer1(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		branch string
	}{
		{"feat branch", "feat/jam-99"},
		{"fix branch", "fix/something"},
		{"empty branch", ""},
		{"main branch", "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, agent.RoleEngineer1, orchestrator.GuessEngineerForTest(tt.branch))
		})
	}
}

func TestParseOpenPRs_SingleValidPR_ReturnsSingleItem(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"feat/jam-55","number":1,"url":"https://github.com/test/pull/1"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	require.Len(t, prs, 1)
}
