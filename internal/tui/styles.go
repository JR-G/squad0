//nolint:misspell // lipgloss.Color is the library's API, not a misspelling
package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Brand colours.
	colorPrimary   = lipgloss.Color("#7C3AED")
	colorSecondary = lipgloss.Color("#A78BFA")
	colorSuccess   = lipgloss.Color("#10B981")
	colorWarning   = lipgloss.Color("#F59E0B")
	colorError     = lipgloss.Color("#EF4444")
	colorMuted     = lipgloss.Color("#6B7280")
	colorWhite     = lipgloss.Color("#FFFFFF")

	// Title renders the Squad0 brand header.
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWhite).
		Background(colorPrimary).
		Padding(0, 2)

	// Subtitle renders section headers.
	Subtitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	// Success renders positive status text.
	Success = lipgloss.NewStyle().
		Foreground(colorSuccess)

	// Warning renders cautionary text.
	Warning = lipgloss.NewStyle().
		Foreground(colorWarning)

	// Error renders error text.
	Error = lipgloss.NewStyle().
		Foreground(colorError)

	// Muted renders de-emphasised text.
	Muted = lipgloss.NewStyle().
		Foreground(colorMuted)

	// AgentName renders agent names.
	AgentName = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	// StatusHealthy renders healthy status.
	StatusHealthy = Success.Bold(true)

	// StatusWorking renders working status.
	StatusWorking = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3B82F6"))

	// StatusIdle renders idle status.
	StatusIdle = Muted

	// StatusFailing renders failing status.
	StatusFailing = Error.Bold(true)

	// Box renders a bordered box.
	Box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2)

	// Prompt renders input prompts.
	Prompt = lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true)

	// Checkmark for completed steps.
	Checkmark = Success.Render("✓")

	// Cross for failed steps.
	Cross = Error.Render("✗")

	// Dot for pending steps.
	Dot = Muted.Render("○")
)
