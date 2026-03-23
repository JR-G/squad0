package orchestrator_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlushSessionMemory_ExtractsAndStoresLearnings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	engDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = engDB.Close() })

	pmDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pmDB.Close() })

	// The extractor returns JSON with escaped quotes inside the result string.
	extractionJSON := `{\"facts\":[{\"entity_name\":\"auth\",\"entity_type\":\"module\",\"content\":\"uses JWT tokens\",\"fact_type\":\"observation\"}],\"beliefs\":[{\"content\":\"always validate tokens server-side\"}],\"entities\":[{\"name\":\"auth\",\"type\":\"module\"}]}`
	extractorRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"` + extractionJSON + `"}` + "\n"),
	}
	extractor := buildAgent(t, extractorRunner, agent.RolePM, pmDB)

	engineerRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	engineer := buildAgent(t, engineerRunner, agent.RoleEngineer1, engDB)

	graphStore := memory.NewGraphStore(engDB)
	factStore := memory.NewFactStore(engDB)
	engineer.SetMemoryStores(graphStore, factStore)

	orchestrator.FlushSessionMemory(ctx, extractor, engineer, "SQ-42", "I fixed the auth module by adding JWT validation.")

	// Verify facts were stored.
	entity, err := graphStore.FindEntityByName(ctx, memory.EntityModule, "auth")
	require.NoError(t, err)
	assert.Equal(t, "auth", entity.Name)

	facts, err := factStore.FactsByEntity(ctx, entity.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, facts)

	// Verify beliefs were stored.
	beliefs, err := factStore.TopBeliefs(ctx, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, beliefs)
}

func TestFlushSessionMemory_ExtractionFails_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	extractorRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"not json at all"}` + "\n"),
	}
	extractor := buildAgent(t, extractorRunner, agent.RolePM, db)

	engineerRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	engineer := buildAgent(t, engineerRunner, agent.RoleEngineer1, db)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	engineer.SetMemoryStores(graphStore, factStore)

	assert.NotPanics(t, func() {
		orchestrator.FlushSessionMemory(ctx, extractor, engineer, "SQ-42", "some transcript")
	})
}

func TestFlushSessionMemory_NilStores_SkipsSafely(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	extractorRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"{}"}` + "\n"),
	}
	extractor := buildAgent(t, extractorRunner, agent.RolePM, db)
	engineer := buildAgent(t, &fakeProcessRunner{output: []byte("{}\n")}, agent.RoleEngineer1, db)
	// Do NOT call SetMemoryStores — stores are nil.

	assert.NotPanics(t, func() {
		orchestrator.FlushSessionMemory(ctx, extractor, engineer, "SQ-42", "transcript")
	})
}

func TestFlushSessionMemory_EmptyTranscript_ExtractsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	extractorRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"{"facts":[],"beliefs":[],"entities":[]}"}` + "\n"),
	}
	extractor := buildAgent(t, extractorRunner, agent.RolePM, db)

	engineer := buildAgent(t, &fakeProcessRunner{output: []byte("{}\n")}, agent.RoleEngineer1, db)
	engineer.SetMemoryStores(memory.NewGraphStore(db), memory.NewFactStore(db))

	assert.NotPanics(t, func() {
		orchestrator.FlushSessionMemory(ctx, extractor, engineer, "SQ-42", "")
	})
}
