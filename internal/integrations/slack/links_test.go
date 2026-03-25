package slack_test

import (
	"testing"

	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
)

func TestTicketLink_WithWorkspace_ReturnsSlackLink(t *testing.T) {
	t.Parallel()

	cfg := slack.LinkConfig{LinearWorkspace: "jamesrg"}

	result := cfg.TicketLink("JAM-17")

	assert.Equal(t, "<https://linear.app/jamesrg/issue/JAM-17|JAM-17>", result)
}

func TestTicketLink_NoWorkspace_ReturnsPlainText(t *testing.T) {
	t.Parallel()

	cfg := slack.LinkConfig{}

	result := cfg.TicketLink("JAM-17")

	assert.Equal(t, "JAM-17", result)
}

func TestPRLink_FormatsShortLink(t *testing.T) {
	t.Parallel()

	cfg := slack.LinkConfig{}

	result := cfg.PRLink("https://github.com/test-org/test-repo/pull/42")

	assert.Equal(t, "<https://github.com/test-org/test-repo/pull/42|PR #42>", result)
}

func TestLinkifyTickets_ReplacesAllTicketIDs(t *testing.T) {
	t.Parallel()

	cfg := slack.LinkConfig{LinearWorkspace: "jamesrg"}

	result := cfg.LinkifyTickets("Working on JAM-17 and JAM-18")

	assert.Contains(t, result, "linear.app/jamesrg/issue/JAM-17")
	assert.Contains(t, result, "linear.app/jamesrg/issue/JAM-18")
}

func TestPRLink_NoNumber_ReturnsOriginal(t *testing.T) {
	t.Parallel()

	cfg := slack.LinkConfig{}

	result := cfg.PRLink("not-a-url")

	assert.Equal(t, "not-a-url", result)
}

func TestLinkifyTickets_NoWorkspace_ReturnsOriginal(t *testing.T) {
	t.Parallel()

	cfg := slack.LinkConfig{}

	result := cfg.LinkifyTickets("Working on JAM-17")

	assert.Equal(t, "Working on JAM-17", result)
}
