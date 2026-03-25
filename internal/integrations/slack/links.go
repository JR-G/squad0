package slack

import (
	"fmt"
	"regexp"
	"strings"
)

// LinkConfig holds the URLs needed to generate ticket and PR links.
type LinkConfig struct {
	LinearWorkspace string // e.g. "jamesrg"
	GitHubOwner     string // e.g. "JR-G"
	GitHubRepo      string // e.g. "makebook"
}

// TicketLink formats a Linear ticket ID as a Slack link.
// "JAM-17" → "<https://linear.app/jamesrg/issue/JAM-17|JAM-17>"
func (cfg LinkConfig) TicketLink(ticket string) string {
	if cfg.LinearWorkspace == "" {
		return ticket
	}
	url := fmt.Sprintf("https://linear.app/%s/issue/%s", cfg.LinearWorkspace, ticket)
	return fmt.Sprintf("<%s|%s>", url, ticket)
}

// PRLink formats a GitHub PR URL as a short Slack link.
// "https://github.com/JR-G/makebook/pull/42" → "<url|PR #42>"
func (cfg LinkConfig) PRLink(prURL string) string {
	number := extractPRNumberFromURL(prURL)
	if number == "" {
		return prURL
	}
	return fmt.Sprintf("<%s|PR #%s>", prURL, number)
}

var ticketPattern = regexp.MustCompile(`[A-Z]+-\d+`)

// LinkifyTickets replaces ticket IDs in text with Slack links.
func (cfg LinkConfig) LinkifyTickets(text string) string {
	if cfg.LinearWorkspace == "" {
		return text
	}

	return ticketPattern.ReplaceAllStringFunc(text, cfg.TicketLink)
}

func extractPRNumberFromURL(prURL string) string {
	idx := strings.LastIndex(prURL, "/")
	if idx == -1 {
		return ""
	}
	return prURL[idx+1:]
}
