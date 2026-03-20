package tui

import "time"

// NewTickMsg creates a tickMsg for testing the dashboard Update method.
func NewTickMsg(tickTime time.Time) tickMsg {
	return tickMsg(tickTime)
}
