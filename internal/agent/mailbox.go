package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

// Message is what a Mailbox processes. Each concrete message type
// owns its own request fields plus a response channel the sender
// reads after the message is handled.
//
// Implemented by ExecuteTaskMsg, DirectSessionMsg, QuickChatMsg —
// add new types as more call sites adopt the actor pattern.
type Message interface {
	IsAgentMessage()
}

// ExecuteTaskMsg drives Agent.ExecuteTask through the mailbox.
type ExecuteTaskMsg struct {
	Task       string
	FilePaths  []string
	WorkingDir string
	Reply      chan<- ExecuteTaskReply
}

// ExecuteTaskReply is what the mailbox sends back after handling
// an ExecuteTaskMsg.
type ExecuteTaskReply struct {
	Result SessionResult
	Err    error
}

// IsAgentMessage marks ExecuteTaskMsg as a Mailbox Message.
func (ExecuteTaskMsg) IsAgentMessage() { _ = 0 }

// DirectSessionMsg drives Agent.DirectSession through the mailbox.
type DirectSessionMsg struct {
	Prompt string
	Reply  chan<- DirectSessionReply
}

// DirectSessionReply is what the mailbox sends back after handling
// a DirectSessionMsg.
type DirectSessionReply struct {
	Result SessionResult
	Err    error
}

// IsAgentMessage marks DirectSessionMsg as a Mailbox Message.
func (DirectSessionMsg) IsAgentMessage() { _ = 0 }

// QuickChatMsg drives Agent.QuickChat through the mailbox.
type QuickChatMsg struct {
	Prompt string
	Reply  chan<- QuickChatReply
}

// QuickChatReply is what the mailbox sends back after handling a
// QuickChatMsg.
type QuickChatReply struct {
	Transcript string
	Err        error
}

// IsAgentMessage marks QuickChatMsg as a Mailbox Message.
func (QuickChatMsg) IsAgentMessage() { _ = 0 }

// ErrMailboxStopped is returned when Send is called after Stop.
var ErrMailboxStopped = errors.New("mailbox is stopped")

// ErrMailboxFull is returned when Send is called and the inbox is at
// capacity. Callers can retry, drop, or escalate as they see fit.
var ErrMailboxFull = errors.New("mailbox inbox is full")

// Mailbox wraps an Agent with channel-based message passing and a
// supervised goroutine that processes one message at a time.
//
// Foundational actor-model layer for #22 of the architecture
// roadmap. The existing Agent methods (ExecuteTask, DirectSession,
// QuickChat) stay valid for direct-call sites; new sites can opt
// into the mailbox for per-agent backpressure, sequencing, and
// future supervision / persistence hooks.
type Mailbox struct {
	agent *Agent
	inbox chan Message
	stop  chan struct{}
	wg    sync.WaitGroup
	once  sync.Once
}

// NewMailbox wraps the given agent. Capacity is the inbox buffer
// size — 0 means unbuffered (every Send blocks until the loop
// reads). 16 is a sensible default for non-test use.
func NewMailbox(agent *Agent, capacity int) *Mailbox {
	if capacity < 0 {
		capacity = 0
	}
	return &Mailbox{
		agent: agent,
		inbox: make(chan Message, capacity),
		stop:  make(chan struct{}),
	}
}

// Start launches the message-processing goroutine. Safe to call
// once; subsequent calls are noops. Panics inside a message
// handler are recovered and logged so one bad message can't crash
// the agent.
func (mb *Mailbox) Start(ctx context.Context) {
	mb.wg.Add(1)
	go mb.loop(ctx)
}

// Send queues a message for processing. Returns ErrMailboxFull if
// the inbox is at capacity (non-blocking) or ErrMailboxStopped if
// Stop was called. The caller should always be prepared for both —
// the actor model says senders never assume delivery.
func (mb *Mailbox) Send(msg Message) error {
	select {
	case <-mb.stop:
		return ErrMailboxStopped
	default:
	}

	select {
	case mb.inbox <- msg:
		return nil
	case <-mb.stop:
		return ErrMailboxStopped
	default:
		return ErrMailboxFull
	}
}

// Stop signals the loop to drain and exit. Safe to call multiple
// times; only the first call has effect. Blocks until the loop
// goroutine has returned.
func (mb *Mailbox) Stop() {
	mb.once.Do(func() {
		close(mb.stop)
	})
	mb.wg.Wait()
}

// Agent returns the wrapped agent for callers that need the
// underlying instance (e.g. accessing memory stores). Calling
// Agent methods directly bypasses the mailbox sequencing — only
// do this for read-only operations.
func (mb *Mailbox) Agent() *Agent {
	return mb.agent
}

// Execute is a synchronous convenience over the request/response
// dance for ExecuteTask. Replaces direct agent.ExecuteTask calls
// at sites that don't need fire-and-forget semantics.
func (mb *Mailbox) Execute(task string, files []string, workDir string) (SessionResult, error) {
	reply := make(chan ExecuteTaskReply, 1)
	if err := mb.Send(ExecuteTaskMsg{Task: task, FilePaths: files, WorkingDir: workDir, Reply: reply}); err != nil {
		return SessionResult{}, err
	}
	r := <-reply
	return r.Result, r.Err
}

// DirectSession is a synchronous convenience over the request/
// response dance for DirectSession. Replaces direct
// agent.DirectSession calls.
func (mb *Mailbox) DirectSession(prompt string) (SessionResult, error) {
	reply := make(chan DirectSessionReply, 1)
	if err := mb.Send(DirectSessionMsg{Prompt: prompt, Reply: reply}); err != nil {
		return SessionResult{}, err
	}
	r := <-reply
	return r.Result, r.Err
}

// QuickChat is a synchronous convenience over the request/response
// dance for QuickChat. Replaces direct agent.QuickChat calls.
func (mb *Mailbox) QuickChat(prompt string) (string, error) {
	reply := make(chan QuickChatReply, 1)
	if err := mb.Send(QuickChatMsg{Prompt: prompt, Reply: reply}); err != nil {
		return "", err
	}
	r := <-reply
	return r.Transcript, r.Err
}

func (mb *Mailbox) loop(ctx context.Context) {
	defer mb.wg.Done()

	for {
		select {
		case <-mb.stop:
			return
		case <-ctx.Done():
			return
		case msg := <-mb.inbox:
			mb.handle(ctx, msg)
		}
	}
}

func (mb *Mailbox) handle(ctx context.Context, msg Message) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("mailbox handler panicked for %s: %v", mb.agent.Role(), recovered)
		}
	}()

	switch concrete := msg.(type) {
	case ExecuteTaskMsg:
		result, err := mb.agent.ExecuteTask(ctx, concrete.Task, concrete.FilePaths, concrete.WorkingDir)
		safeReply(concrete.Reply, ExecuteTaskReply{Result: result, Err: err})
	case DirectSessionMsg:
		result, err := mb.agent.DirectSession(ctx, concrete.Prompt)
		safeReply(concrete.Reply, DirectSessionReply{Result: result, Err: err})
	case QuickChatMsg:
		transcript, err := mb.agent.QuickChat(ctx, concrete.Prompt)
		safeReply(concrete.Reply, QuickChatReply{Transcript: transcript, Err: err})
	default:
		log.Printf("mailbox received unknown message type %T for %s", msg, mb.agent.Role())
	}
}

// safeReply sends to reply without blocking forever if the receiver
// has gone away. Bounded by the channel's capacity — for unbuffered
// channels the send happens iff someone's reading.
func safeReply[T any](reply chan<- T, value T) {
	if reply == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			// Reply channel was closed by sender — they no longer
			// care about the response. That's fine.
			_ = fmt.Sprint(recovered)
		}
	}()
	reply <- value
}
