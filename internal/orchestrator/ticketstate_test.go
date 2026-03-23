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

func TestMoveTicketState_CallsPMAgent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	pmAgent := buildAgent(t, runner, agent.RolePM, db)

	orchestrator.MoveTicketState(ctx, pmAgent, "JAM-42", "In Progress")

	require.Len(t, runner.calls, 1)
	assert.Contains(t, runner.calls[0].stdin, "JAM-42")
	assert.Contains(t, runner.calls[0].stdin, "In Progress")
}

func TestMoveTicketState_NilPM_DoesNotPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		orchestrator.MoveTicketState(context.Background(), nil, "JAM-1", "Done")
	})
}

func TestMoveTicketState_SessionError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"rate limited"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := buildAgent(t, runner, agent.RolePM, db)

	assert.NotPanics(t, func() {
		orchestrator.MoveTicketState(ctx, pmAgent, "JAM-1", "In Review")
	})
}
