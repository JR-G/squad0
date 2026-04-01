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

func TestParseOpenPRs_ValidJSON(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"feat/jam-12","number":6,"url":"https://github.com/JR-G/makebook/pull/6"},{"headRefName":"feat/jam-9","number":10,"url":"https://github.com/JR-G/makebook/pull/10"}]`

	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Len(t, prs, 2)
}

func TestParseOpenPRs_NoBranch_Skipped(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"main","number":1,"url":"https://github.com/test/repo/pull/1"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Empty(t, prs)
}

func TestParseOpenPRs_Empty(t *testing.T) {
	t.Parallel()

	prs := orchestrator.ParseOpenPRsForTest("[]")
	assert.Empty(t, prs)
}

func TestGuessEngineer_ReturnsDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, agent.RoleEngineer1, orchestrator.GuessEngineerForTest("feat/jam-12"))
}

func TestHasPipelineItem_NoStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	assert.False(t, orch.HasPipelineItemForTest(ctx, "JAM-MISSING"))

	_, _ = pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-EXISTS", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	assert.True(t, orch.HasPipelineItemForTest(ctx, "JAM-EXISTS"))
}

func TestRecoverOrphanedPRs_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	orch.RecoverOrphanedPRsForTest(context.Background())
}

func TestExtractJSONField(t *testing.T) {
	t.Parallel()

	line := `{"headRefName":"feat/jam-12","url":"https://github.com/test/pull/6"}`
	assert.Equal(t, "feat/jam-12", orchestrator.ExtractJSONFieldForTest(line, "headRefName"))
	assert.Equal(t, "https://github.com/test/pull/6", orchestrator.ExtractJSONFieldForTest(line, "url"))
	assert.Empty(t, orchestrator.ExtractJSONFieldForTest(line, "missing"))
}
