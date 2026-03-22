package orchestrator_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPMBriefing_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Team, let's get started."}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, db)
	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	assert.NotPanics(t, func() {
		orchestrator.RunPMBriefing(ctx, agents, nil)
	})
}

func TestRunPMBriefing_NoPM_DoesNotPanic(t *testing.T) {
	t.Parallel()

	agents := map[agent.Role]*agent.Agent{}

	assert.NotPanics(t, func() {
		orchestrator.RunPMBriefing(context.Background(), agents, nil)
	})
}

func TestRunPMBriefing_SessionFails_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","result":"failed"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}
	pmAgent := buildAgent(t, runner, agent.RolePM, db)
	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	assert.NotPanics(t, func() {
		orchestrator.RunPMBriefing(ctx, agents, nil)
	})
}

func TestRunPMBriefing_WithBot_PostsToEngineering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Morning team."}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, db)
	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestSlackBot(server.URL)

	assert.NotPanics(t, func() {
		orchestrator.RunPMBriefing(ctx, agents, bot)
	})
}
