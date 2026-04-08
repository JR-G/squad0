package orchestrator

import "strings"

// deferralSignals are words that indicate the PM wants to hold a ticket.
var deferralSignals = []string{
	"defer",
	"deferred",
	"skip",
	"hold",
	"wait",
	"not yet",
	"stays deferred",
	"don't assign",
	"do not assign",
	"stop",
	"paused",
}

// containsDeferralSignal returns true if the text contains any
// deferral-related words.
func containsDeferralSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, signal := range deferralSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// ticketMentionedNearDeferral returns true if the ticket ID appears
// within 50 characters of a deferral signal in the text. This avoids
// false positives where the PM defers one ticket but mentions another
// in passing.
func ticketMentionedNearDeferral(text, ticket string) bool {
	lower := strings.ToLower(text)
	ticketLower := strings.ToLower(ticket)

	ticketIdx := strings.Index(lower, ticketLower)
	if ticketIdx < 0 {
		return false
	}

	// Check a window around the ticket mention for deferral signals.
	windowStart := ticketIdx - 50
	if windowStart < 0 {
		windowStart = 0
	}
	windowEnd := ticketIdx + len(ticket) + 50
	if windowEnd > len(lower) {
		windowEnd = len(lower)
	}

	window := lower[windowStart:windowEnd]
	for _, signal := range deferralSignals {
		if strings.Contains(window, signal) {
			return true
		}
	}

	return false
}

// ContainsDeferralSignalForTest exports containsDeferralSignal for testing.
func ContainsDeferralSignalForTest(text string) bool {
	return containsDeferralSignal(text)
}

// TicketMentionedNearDeferralForTest exports ticketMentionedNearDeferral for testing.
func TicketMentionedNearDeferralForTest(text, ticket string) bool {
	return ticketMentionedNearDeferral(text, ticket)
}
