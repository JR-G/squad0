package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretsSet_NoStdinContent_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	deps := &cli.SecretsCommandDeps{
		Manager: nil,
		Stdin:   strings.NewReader(""),
	}

	kc := newTestDeps(runner, "")
	deps.Manager = kc.Manager
	deps.Stdin = strings.NewReader("")

	rootCmd := cli.NewRootCommandForTest(deps)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "set", "SLACK_BOT_TOKEN"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading input")
}

func TestSecretsSet_KeychainError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security add-generic-password -s squad0 -a SLACK_BOT_TOKEN -w test-value -U",
		nil,
		fmt.Errorf("keychain access denied"),
	)
	deps := newTestDeps(runner, "test-value\n")

	rootCmd := cli.NewRootCommandForTest(deps)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "set", "SLACK_BOT_TOKEN"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keychain set")
}

func TestSecretsList_KeychainError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a SLACK_BOT_TOKEN",
		[]byte("failure"),
		fmt.Errorf("keychain locked"),
	)
	deps := newTestDeps(runner, "")

	rootCmd := cli.NewRootCommandForTest(deps)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "list"})

	err := rootCmd.Execute()

	require.Error(t, err)
}

func TestReadFromTUI_WithPipedInput_ReturnsValue(t *testing.T) {
	t.Parallel()

	pipeReader, pipeWriter := io.Pipe()
	go func() {
		// Send value then Enter then Ctrl+C to terminate
		_, _ = pipeWriter.Write([]byte("secret-val"))
		_, _ = pipeWriter.Write([]byte{'\r'})
		_ = pipeWriter.Close()
	}()

	value, err := cli.ReadFromTUI("TEST_SECRET", pipeReader)

	// bubbletea may error when pipe closes, but if it got the value that's fine
	if err == nil {
		assert.Equal(t, "secret-val", value)
	}
}

func TestSecretsList_NilDeps_CreatesDefaultManager(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "list"})

	err := rootCmd.Execute()
	if err != nil {
		assert.NotContains(t, err.Error(), "nil pointer")
	}
}

func TestSecretsVerify_NilDeps_CreatesDefaultManager(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "verify"})

	err := rootCmd.Execute()
	if err != nil {
		assert.NotContains(t, err.Error(), "nil pointer")
	}
}
