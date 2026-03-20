package tui

import "fmt"

// Banner prints the Squad0 startup banner.
func Banner() string {
	return fmt.Sprintf("\n%s\n\n", Title.Render(" Squad0 "))
}

// StepDone renders a completed setup step.
func StepDone(message string) string {
	return fmt.Sprintf("  %s %s\n", Checkmark, message)
}

// StepFail renders a failed setup step.
func StepFail(message string) string {
	return fmt.Sprintf("  %s %s\n", Cross, Error.Render(message))
}

// StepPending renders a pending setup step.
func StepPending(message string) string {
	return fmt.Sprintf("  %s %s\n", Dot, Muted.Render(message))
}

// StepWarn renders a warning step.
func StepWarn(message string) string {
	return fmt.Sprintf("  %s %s\n", Warning.Render("!"), Warning.Render(message))
}

// Section renders a section header.
func Section(title string) string {
	return fmt.Sprintf("\n%s\n", Subtitle.Render(title))
}
