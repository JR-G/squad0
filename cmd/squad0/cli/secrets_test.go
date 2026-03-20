package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "squad0.toml")
	content := `[project]
name = "squad0"
`
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

type fakeRunner struct {
	responses map[string]fakeResponse
}

type fakeResponse struct {
	output []byte
	err    error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: make(map[string]fakeResponse)}
}

func (runner *fakeRunner) On(args string, output []byte, err error) {
	runner.responses[args] = fakeResponse{output: output, err: err}
}

func (runner *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")

	resp, ok := runner.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}

	return resp.output, resp.err
}

func newTestDeps(runner *fakeRunner, stdinContent string) *cli.SecretsCommandDeps {
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	mgr := secrets.NewManager(kc)

	return &cli.SecretsCommandDeps{
		Manager: mgr,
		Stdin:   strings.NewReader(stdinContent),
	}
}

func TestSecretsSet_ValidInput_StoresSecret(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security add-generic-password -s squad0 -a SLACK_BOT_TOKEN -w test-token -U", nil, nil)
	deps := newTestDeps(runner, "test-token\n")

	rootCmd := cli.NewRootCommandForTest(deps)
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "set", "SLACK_BOT_TOKEN"})

	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, output.String(), "stored successfully")
}

func TestSecretsSet_EmptyInput_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	deps := newTestDeps(runner, "\n")

	rootCmd := cli.NewRootCommandForTest(deps)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "set", "SLACK_BOT_TOKEN"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestSecretsSet_InvalidName_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	deps := newTestDeps(runner, "value\n")

	rootCmd := cli.NewRootCommandForTest(deps)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "set", "INVALID_NAME"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognised")
}

func TestSecretsList_ShowsStatus(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN", []byte("found\n"), nil)
	notFound := []byte("could not be found")
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN", notFound, &exec.ExitError{})
	deps := newTestDeps(runner, "")

	rootCmd := cli.NewRootCommandForTest(deps)
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "list"})

	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, output.String(), "SLACK_BOT_TOKEN")
	assert.Contains(t, output.String(), "[set]")
	assert.Contains(t, output.String(), "SLACK_APP_TOKEN")
	assert.Contains(t, output.String(), "[not set]")
}

func TestSecretsVerify_AllPresent_Succeeds(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN", []byte("found\n"), nil)
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN", []byte("found\n"), nil)
	deps := newTestDeps(runner, "")

	rootCmd := cli.NewRootCommandForTest(deps)
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "verify"})

	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, output.String(), "All required secrets are configured")
}

func TestSecretsVerify_SomeMissing_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN", []byte("found\n"), nil)
	notFound := []byte("could not be found")
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN", notFound, &exec.ExitError{})
	deps := newTestDeps(runner, "")

	rootCmd := cli.NewRootCommandForTest(deps)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"secrets", "verify"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SLACK_APP_TOKEN")
}

func TestConfigValidate_ValidFile_Succeeds(t *testing.T) {
	t.Parallel()

	configPath := writeTestConfig(t)
	rootCmd := cli.NewRootCommand()
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "validate", "--config", configPath})

	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, output.String(), "Configuration valid")
}

func TestConfigValidate_MissingFile_ReturnsError(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "validate", "--config", "/nonexistent/file.toml"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration invalid")
}
