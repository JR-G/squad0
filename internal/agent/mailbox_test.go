package agent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMailbox_Send_DeliversAndProcesses(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 4)
	mb.Start(context.Background())
	t.Cleanup(mb.Stop)

	reply := make(chan agent.DirectSessionReply, 1)
	require.NoError(t, mb.Send(agent.DirectSessionMsg{
		Prompt: "say hi",
		Reply:  reply,
	}))

	select {
	case got := <-reply:
		// Underlying agent uses a fake runner that returns nil error;
		// either path is fine — we just want the reply to arrive.
		_ = got
	case <-time.After(2 * time.Second):
		t.Fatal("reply never arrived")
	}
}

func TestMailbox_Send_AfterStop_ReturnsErr(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 1)
	mb.Start(context.Background())
	mb.Stop()

	err := mb.Send(agent.DirectSessionMsg{Prompt: "x"})

	assert.True(t, errors.Is(err, agent.ErrMailboxStopped))
}

func TestMailbox_Send_FullInbox_ReturnsErr(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 0) // unbuffered
	// Don't Start — inbox can't drain.

	err := mb.Send(agent.DirectSessionMsg{Prompt: "x"})

	assert.True(t, errors.Is(err, agent.ErrMailboxFull))
}

func TestMailbox_Stop_IsIdempotent(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 1)
	mb.Start(context.Background())

	assert.NotPanics(t, func() {
		mb.Stop()
		mb.Stop()
		mb.Stop()
	})
}

func TestMailbox_Stop_BlocksUntilLoopExits(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 1)

	var loopExited atomic.Bool
	mb.Start(context.Background())

	go func() {
		mb.Stop()
		loopExited.Store(true)
	}()

	require.Eventually(t, loopExited.Load, time.Second, 5*time.Millisecond)
}

func TestMailbox_Send_NegativeCapacity_TreatedAsZero(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, -10)
	t.Cleanup(mb.Stop)

	// No Start — unbuffered + no consumer = full.
	err := mb.Send(agent.DirectSessionMsg{Prompt: "x"})
	assert.True(t, errors.Is(err, agent.ErrMailboxFull))
}

func TestMailbox_Agent_ReturnsWrappedAgent(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 1)
	t.Cleanup(mb.Stop)

	assert.Same(t, agentInstance, mb.Agent())
}

func TestMailbox_NilReply_DoesNotPanic(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 1)
	mb.Start(context.Background())
	t.Cleanup(mb.Stop)

	// Reply nil — handler should not panic, just discard the result.
	require.NoError(t, mb.Send(agent.DirectSessionMsg{Prompt: "x"}))

	// Give the loop a moment to process before stopping.
	time.Sleep(50 * time.Millisecond)
}

func TestMailbox_HandlesAllKnownMessageTypes(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 4)
	mb.Start(context.Background())
	t.Cleanup(mb.Stop)

	execReply := make(chan agent.ExecuteTaskReply, 1)
	require.NoError(t, mb.Send(agent.ExecuteTaskMsg{Task: "do x", Reply: execReply}))
	select {
	case <-execReply:
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteTaskMsg never replied")
	}

	chatReply := make(chan agent.QuickChatReply, 1)
	require.NoError(t, mb.Send(agent.QuickChatMsg{Prompt: "yo", Reply: chatReply}))
	select {
	case <-chatReply:
	case <-time.After(2 * time.Second):
		t.Fatal("QuickChatMsg never replied")
	}
}

func TestMailbox_MessageMarkerMethods_AreNoOps(t *testing.T) {
	t.Parallel()

	// The unexported marker method exists purely to constrain the
	// Message interface; calling it directly verifies coverage and
	// documents the intent.
	assert.NotPanics(t, func() {
		agent.ExecuteTaskMsg{}.IsAgentMessage()
		agent.DirectSessionMsg{}.IsAgentMessage()
		agent.QuickChatMsg{}.IsAgentMessage()
	})
}

func TestMailbox_ContextCancellation_StopsLoop(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	mb := agent.NewMailbox(agentInstance, 1)
	ctx, cancel := context.WithCancel(context.Background())
	mb.Start(ctx)

	cancel()

	// Stop should return promptly because the loop noticed the
	// cancelled context.
	done := make(chan struct{})
	go func() {
		mb.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked after ctx cancellation")
	}
}
