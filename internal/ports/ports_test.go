package ports_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/ports"
)

// These tests are compile-time guards: they assert that any future
// type can be used to satisfy the port interfaces, by binding empty
// stub implementations to interface variables. If the port surface
// changes incompatibly the build breaks here, before any adapter
// drifts out of sync.

type stubPRHost struct{}

func (stubPRHost) State(context.Context, string) (ports.PRState, error)        { return ports.PRState{}, nil }
func (stubPRHost) Reviews(context.Context, string) ([]ports.PRReview, error)   { return nil, nil }
func (stubPRHost) Comments(context.Context, string) ([]ports.PRComment, error) { return nil, nil }
func (stubPRHost) Commits(context.Context, string) ([]ports.PRCommit, error)   { return nil, nil }
func (stubPRHost) List(context.Context, ports.PRListFilter) ([]ports.PRListing, error) {
	return nil, nil
}
func (stubPRHost) Comment(context.Context, string, string) error { return nil }
func (stubPRHost) Merge(context.Context, string) error           { return nil }

type stubTicketSource struct{}

func (stubTicketSource) ListReady(context.Context) ([]ports.Ticket, error) { return nil, nil }
func (stubTicketSource) Get(context.Context, string) (ports.Ticket, error) {
	return ports.Ticket{}, nil
}
func (stubTicketSource) UpdateState(context.Context, string, string) error   { return nil }
func (stubTicketSource) CreateComment(context.Context, string, string) error { return nil }

type stubChatPlatform struct{}

func (stubChatPlatform) Post(context.Context, string, string, ports.SenderIdentity) (string, error) {
	return "", nil
}

func (stubChatPlatform) PostThreadReply(context.Context, string, string, string, ports.SenderIdentity) error {
	return nil
}

func (stubChatPlatform) History(context.Context, string, int) ([]ports.IncomingMessage, error) {
	return nil, nil
}
func (stubChatPlatform) Listen(context.Context, ports.MessageHandler) error { return nil }

func TestPullRequestHost_StubSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ ports.PullRequestHost = stubPRHost{}
}

func TestTicketSource_StubSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ ports.TicketSource = stubTicketSource{}
}

func TestChatPlatform_StubSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ ports.ChatPlatform = stubChatPlatform{}
}
