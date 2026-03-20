package slack_test

import (
	"testing"

	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCommand_SimpleCommand_ReturnsCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		name    string
		argsLen int
	}{
		{"start", "start", 0},
		{"stop", "stop", 0},
		{"status", "status", 0},
		{"standup", "standup", 0},
		{"retro", "retro", 0},
		{"agents", "agents", 0},
		{"feed", "feed", 0},
		{"problems", "problems", 0},
		{"health", "health", 0},
		{"version", "version", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			cmd, err := slack.ParseCommand(tt.input)

			require.NoError(t, err)
			assert.Equal(t, tt.name, cmd.Name)
			assert.Len(t, cmd.Args, tt.argsLen)
		})
	}
}

func TestParseCommand_CommandWithArgs_ParsesArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input        string
		name         string
		expectedArgs []string
	}{
		{"assign SQ-42 engineer-1", "assign", []string{"SQ-42", "engineer-1"}},
		{"pause engineer-2", "pause", []string{"engineer-2"}},
		{"resume", "resume", nil},
		{"discuss SQ-100", "discuss", []string{"SQ-100"}},
		{"memory engineer-1", "memory", []string{"engineer-1"}},
		{"merge-mode auto", "merge-mode", []string{"auto"}},
		{"merge-mode auto 2h", "merge-mode", []string{"auto", "2h"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			cmd, err := slack.ParseCommand(tt.input)

			require.NoError(t, err)
			assert.Equal(t, tt.name, cmd.Name)
			assert.Equal(t, tt.expectedArgs, cmd.Args)
		})
	}
}

func TestParseCommand_CaseInsensitive(t *testing.T) {
	t.Parallel()

	cmd, err := slack.ParseCommand("START")

	require.NoError(t, err)
	assert.Equal(t, "start", cmd.Name)
}

func TestParseCommand_WhitespaceHandling(t *testing.T) {
	t.Parallel()

	cmd, err := slack.ParseCommand("  status  ")

	require.NoError(t, err)
	assert.Equal(t, "status", cmd.Name)
}

func TestParseCommand_EmptyInput_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := slack.ParseCommand("")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")
}

func TestParseCommand_UnknownCommand_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := slack.ParseCommand("deploy")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognised command")
	assert.Contains(t, err.Error(), "deploy")
}

func TestParseCommand_UnknownCommand_ListsValidCommands(t *testing.T) {
	t.Parallel()

	_, err := slack.ParseCommand("invalid")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start")
	assert.Contains(t, err.Error(), "stop")
}

func TestValidCommands_ReturnsAll(t *testing.T) {
	t.Parallel()

	commands := slack.ValidCommands()

	assert.GreaterOrEqual(t, len(commands), 15)
}
