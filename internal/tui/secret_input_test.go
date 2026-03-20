package tui_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretInput_Init_ReturnsBlink(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("SLACK_BOT_TOKEN")
	cmd := model.Init()

	assert.NotNil(t, cmd)
}

func TestSecretInput_View_ShowsPromptAndSecretName(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("SLACK_BOT_TOKEN")
	view := model.View()

	assert.Contains(t, view, "SLACK_BOT_TOKEN")
	assert.Contains(t, view, "Enter value for")
	assert.Contains(t, view, "enter to confirm")
	assert.Contains(t, view, "esc to cancel")
}

func TestSecretInput_EnterKey_SetsValueAndQuits(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST_SECRET")

	// Type some characters first.
	charMsgs := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'s'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'c'}},
	}
	var updated tea.Model = model
	for _, msg := range charMsgs {
		updated, _ = updated.Update(msg)
	}

	// Press Enter.
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	final, cmd := updated.Update(enterMsg)

	require.NotNil(t, cmd)
	finalModel, ok := final.(tui.SecretInputModel)
	require.True(t, ok)
	assert.Equal(t, "sec", finalModel.Value())
	assert.False(t, finalModel.Aborted())
}

func TestSecretInput_EscKey_AbortsWithoutValue(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST_SECRET")

	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	final, cmd := model.Update(escMsg)

	require.NotNil(t, cmd)
	finalModel, ok := final.(tui.SecretInputModel)
	require.True(t, ok)
	assert.True(t, finalModel.Aborted())
	assert.Empty(t, finalModel.Value())
}

func TestSecretInput_CtrlC_AbortsWithoutValue(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST_SECRET")

	ctrlCMsg := tea.KeyMsg{Type: tea.KeyCtrlC}
	final, cmd := model.Update(ctrlCMsg)

	require.NotNil(t, cmd)
	finalModel, ok := final.(tui.SecretInputModel)
	require.True(t, ok)
	assert.True(t, finalModel.Aborted())
	assert.Empty(t, finalModel.Value())
}

func TestSecretInput_EnterEmpty_SetsEmptyValue(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST_SECRET")

	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	final, _ := model.Update(enterMsg)

	finalModel, ok := final.(tui.SecretInputModel)
	require.True(t, ok)
	assert.Empty(t, finalModel.Value())
	assert.False(t, finalModel.Aborted())
}

func TestSecretInput_View_AfterDone_ShowsSaved(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("MY_KEY")

	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	final, _ := model.Update(enterMsg)

	finalModel, ok := final.(tui.SecretInputModel)
	require.True(t, ok)

	view := finalModel.View()
	assert.Contains(t, view, "MY_KEY")
	assert.Contains(t, view, "saved")
}

func TestSecretInput_NonKeyMsg_PassesToTextInput(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST_SECRET")

	// Send a window size message (non-key).
	windowMsg := tea.WindowSizeMsg{Width: 80, Height: 24}
	updated, _ := model.Update(windowMsg)

	finalModel, ok := updated.(tui.SecretInputModel)
	require.True(t, ok)
	assert.False(t, finalModel.Aborted())
	assert.Empty(t, finalModel.Value())
}

func TestSecretInput_RegularKey_DoesNotQuit(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST_SECRET")

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	updated, cmd := model.Update(keyMsg)

	finalModel, ok := updated.(tui.SecretInputModel)
	require.True(t, ok)
	assert.False(t, finalModel.Aborted())
	assert.Empty(t, finalModel.Value())

	// The cmd should not be tea.Quit (it may be nil or a blink cmd).
	if cmd != nil {
		quitMsg := cmd()
		_, isQuit := quitMsg.(tea.QuitMsg)
		assert.False(t, isQuit)
	}
}

func TestSecretInput_Value_InitiallyEmpty(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST")

	assert.Empty(t, model.Value())
}

func TestSecretInput_Aborted_InitiallyFalse(t *testing.T) {
	t.Parallel()

	model := tui.NewSecretInput("TEST")

	assert.False(t, model.Aborted())
}
