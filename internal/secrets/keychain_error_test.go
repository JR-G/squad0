package secrets_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/JR-G/squad0/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecRunner_Run_ValidCommand_ReturnsOutput(t *testing.T) {
	t.Parallel()

	runner := secrets.ExecRunner{}
	output, err := runner.Run(context.Background(), "echo", "hello")

	require.NoError(t, err)
	assert.Contains(t, string(output), "hello")
}

func TestExecRunner_Run_InvalidCommand_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := secrets.ExecRunner{}
	_, err := runner.Run(context.Background(), "nonexistent-binary-xyz-123")

	require.Error(t, err)
}

func TestKeychain_Exists_NonItemNotFoundError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a BROKEN_KEY",
		[]byte("some other error output"),
		fmt.Errorf("unexpected error"),
	)
	kc := secrets.NewKeychain("squad0", runner)

	_, err := kc.Exists(context.Background(), "BROKEN_KEY")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keychain exists BROKEN_KEY")
}

func TestKeychain_Delete_NonItemNotFoundError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security delete-generic-password -s squad0 -a BROKEN_KEY",
		[]byte("some other error output"),
		fmt.Errorf("unexpected error"),
	)
	kc := secrets.NewKeychain("squad0", runner)

	err := kc.Delete(context.Background(), "BROKEN_KEY")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keychain delete BROKEN_KEY")
	assert.NotErrorIs(t, err, secrets.ErrSecretNotFound)
}

func TestKeychain_Get_ExitCode44_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 44")
	err := cmd.Run()

	var exitErr *exec.ExitError
	if assert.ErrorAs(t, err, &exitErr) {
		assert.Equal(t, 44, exitErr.ExitCode())
	}

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a EXIT44 -w",
		nil,
		err,
	)
	kc := secrets.NewKeychain("squad0", runner)

	_, kcErr := kc.Get(context.Background(), "EXIT44")

	require.ErrorIs(t, kcErr, secrets.ErrSecretNotFound)
}

func TestKeychain_Exists_ExitCode44_ReturnsFalse(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 44")
	err := cmd.Run()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a EXIT44",
		nil,
		err,
	)
	kc := secrets.NewKeychain("squad0", runner)

	exists, kcErr := kc.Exists(context.Background(), "EXIT44")

	require.NoError(t, kcErr)
	assert.False(t, exists)
}

func TestKeychain_Delete_ExitCode44_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 44")
	err := cmd.Run()

	runner := newFakeRunner()
	runner.On(
		"security delete-generic-password -s squad0 -a EXIT44",
		nil,
		err,
	)
	kc := secrets.NewKeychain("squad0", runner)

	kcErr := kc.Delete(context.Background(), "EXIT44")

	require.ErrorIs(t, kcErr, secrets.ErrSecretNotFound)
}
