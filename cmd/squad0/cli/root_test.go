package cli_test

import (
	"bytes"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCommand_HasExpectedSubcommands(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()

	commands := rootCmd.Commands()
	names := make([]string, 0, len(commands))
	for _, cmd := range commands {
		names = append(names, cmd.Name())
	}

	assert.Contains(t, names, "config")
	assert.Contains(t, names, "secrets")
	assert.Contains(t, names, "version")
}

func TestNewRootCommand_ConfigFlagDefault(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()

	flag := rootCmd.PersistentFlags().Lookup("config")

	require.NotNil(t, flag)
	assert.Equal(t, "config/squad0.toml", flag.DefValue)
}

func TestVersionCommand_PrintsVersion(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetArgs([]string{"version"})

	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, output.String(), "squad0 version")
}

func TestSecretsCommand_HasExpectedSubcommands(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	secretsCmd := findSubcommand(rootCmd, "secrets")
	require.NotNil(t, secretsCmd, "secrets command not found")

	subcommands := secretsCmd.Commands()
	names := make([]string, 0, len(subcommands))
	for _, sub := range subcommands {
		names = append(names, sub.Name())
	}

	assert.Contains(t, names, "set")
	assert.Contains(t, names, "list")
	assert.Contains(t, names, "verify")
}

func findSubcommand(root *cobra.Command, name string) *cobra.Command {
	for _, cmd := range root.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
