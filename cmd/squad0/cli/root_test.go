package cli_test

import (
	"bytes"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCommand_HasExpectedSubcommands(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()

	var names []string
	for _, cmd := range rootCmd.Commands() {
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

	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "secrets" {
			var names []string
			for _, sub := range cmd.Commands() {
				names = append(names, sub.Name())
			}
			assert.Contains(t, names, "set")
			assert.Contains(t, names, "list")
			assert.Contains(t, names, "verify")
			return
		}
	}

	t.Fatal("secrets command not found")
}
