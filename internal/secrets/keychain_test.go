package secrets_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResponse struct {
	output []byte
	err    error
}

type fakeRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		responses: make(map[string]fakeResponse),
	}
}

func (runner *fakeRunner) On(args string, output []byte, err error) {
	runner.responses[args] = fakeResponse{output: output, err: err}
}

func (runner *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	runner.calls = append(runner.calls, key)

	resp, ok := runner.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}

	return resp.output, resp.err
}

func TestKeychain_Get_ExistingSecret_ReturnsValue(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a MY_SECRET -w",
		[]byte("secret-value\n"),
		nil,
	)
	kc := secrets.NewKeychain("squad0", runner)

	value, err := kc.Get(context.Background(), "MY_SECRET")

	require.NoError(t, err)
	assert.Equal(t, "secret-value", value)
}

func TestKeychain_Get_MissingSecret_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a MISSING -w",
		[]byte("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n"),
		&exec.ExitError{},
	)
	kc := secrets.NewKeychain("squad0", runner)

	_, err := kc.Get(context.Background(), "MISSING")

	require.ErrorIs(t, err, secrets.ErrSecretNotFound)
}

func TestKeychain_Get_CommandFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a BROKEN -w",
		nil,
		fmt.Errorf("command failed"),
	)
	kc := secrets.NewKeychain("squad0", runner)

	_, err := kc.Get(context.Background(), "BROKEN")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keychain get BROKEN")
}

func TestKeychain_Set_NewSecret_Succeeds(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security add-generic-password -s squad0 -a MY_SECRET -w my-value -U",
		nil,
		nil,
	)
	kc := secrets.NewKeychain("squad0", runner)

	err := kc.Set(context.Background(), "MY_SECRET", "my-value")

	require.NoError(t, err)
	assert.Len(t, runner.calls, 1)
}

func TestKeychain_Set_CommandFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security add-generic-password -s squad0 -a MY_SECRET -w val -U",
		nil,
		fmt.Errorf("permission denied"),
	)
	kc := secrets.NewKeychain("squad0", runner)

	err := kc.Set(context.Background(), "MY_SECRET", "val")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keychain set MY_SECRET")
}

func TestKeychain_Exists_Present_ReturnsTrue(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a PRESENT",
		[]byte("keychain: \"/Users/test/Library/Keychains/login.keychain-db\"\n"),
		nil,
	)
	kc := secrets.NewKeychain("squad0", runner)

	exists, err := kc.Exists(context.Background(), "PRESENT")

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestKeychain_Exists_Absent_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a ABSENT",
		[]byte("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n"),
		&exec.ExitError{},
	)
	kc := secrets.NewKeychain("squad0", runner)

	exists, err := kc.Exists(context.Background(), "ABSENT")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestKeychain_Delete_ExistingSecret_Succeeds(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security delete-generic-password -s squad0 -a MY_SECRET",
		nil,
		nil,
	)
	kc := secrets.NewKeychain("squad0", runner)

	err := kc.Delete(context.Background(), "MY_SECRET")

	require.NoError(t, err)
}

func TestKeychain_Delete_MissingSecret_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security delete-generic-password -s squad0 -a MISSING",
		[]byte("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n"),
		&exec.ExitError{},
	)
	kc := secrets.NewKeychain("squad0", runner)

	err := kc.Delete(context.Background(), "MISSING")

	require.ErrorIs(t, err, secrets.ErrSecretNotFound)
}
