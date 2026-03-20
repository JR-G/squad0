package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// SecretInputModel is a bubbletea model for securely entering a secret.
type SecretInputModel struct {
	input   textinput.Model
	name    string
	value   string
	done    bool
	aborted bool
}

// NewSecretInput creates a model for entering a secret value.
func NewSecretInput(secretName string) SecretInputModel {
	input := textinput.New()
	input.Placeholder = "paste or type value"
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.Focus()

	return SecretInputModel{
		input: input,
		name:  secretName,
	}
}

// Init initialises the bubbletea model.
func (model SecretInputModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles input events.
func (model SecretInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		model.input, cmd = model.input.Update(msg)
		return model, cmd
	}

	return model.handleKey(keyMsg)
}

func (model SecretInputModel) handleKey(keyMsg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch keyMsg.Type { //nolint:exhaustive // only handling actionable keys
	case tea.KeyEnter:
		model.value = model.input.Value()
		model.done = true
		return model, tea.Quit
	case tea.KeyCtrlC, tea.KeyEsc:
		model.aborted = true
		return model, tea.Quit
	default:
		var cmd tea.Cmd
		model.input, cmd = model.input.Update(keyMsg)
		return model, cmd
	}
}

// View renders the secret input UI.
func (model SecretInputModel) View() string {
	if model.done {
		return fmt.Sprintf("  %s %s %s\n", Checkmark, model.name, Success.Render("saved"))
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "\n%s %s\n\n", Prompt.Render("Enter value for"), model.name)
	fmt.Fprintf(&builder, "  %s\n\n", model.input.View())
	builder.WriteString(Muted.Render("  enter to confirm • esc to cancel"))
	builder.WriteString("\n")

	return builder.String()
}

// Value returns the entered secret value.
func (model SecretInputModel) Value() string {
	return model.value
}

// Aborted returns whether the user cancelled input.
func (model SecretInputModel) Aborted() bool {
	return model.aborted
}
