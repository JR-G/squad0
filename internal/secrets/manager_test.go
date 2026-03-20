package secrets_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/JR-G/squad0/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(runner *fakeRunner) *secrets.Manager {
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	return secrets.NewManager(kc)
}

func TestManager_LoadAll_AllPresent_ReturnsSecrets(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN -w", []byte("xoxb-bot\n"), nil)
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN -w", []byte("xapp-app\n"), nil)
	mgr := newTestManager(runner)

	result, err := mgr.LoadAll(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "xoxb-bot", result.SlackBotToken)
	assert.Equal(t, "xapp-app", result.SlackAppToken)
}

func TestManager_LoadAll_SomeMissing_ReturnsErrorListingAll(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	notFound := []byte("could not be found")
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN -w", notFound, &exec.ExitError{})
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN -w", notFound, &exec.ExitError{})
	mgr := newTestManager(runner)

	_, err := mgr.LoadAll(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SLACK_BOT_TOKEN")
	assert.Contains(t, err.Error(), "SLACK_APP_TOKEN")
}

func TestManager_LoadAll_OneMissing_ReturnsErrorWithMissingName(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN -w", []byte("xoxb-bot\n"), nil)
	notFound := []byte("could not be found")
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN -w", notFound, &exec.ExitError{})
	mgr := newTestManager(runner)

	_, err := mgr.LoadAll(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SLACK_APP_TOKEN")
	assert.NotContains(t, err.Error(), "SLACK_BOT_TOKEN")
}

func TestManager_Verify_AllPresent_ReturnsNilError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN", []byte("found\n"), nil)
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN", []byte("found\n"), nil)
	mgr := newTestManager(runner)

	status, err := mgr.Verify(context.Background())

	require.NoError(t, err)
	assert.True(t, status["SLACK_BOT_TOKEN"])
	assert.True(t, status["SLACK_APP_TOKEN"])
}

func TestManager_Verify_SomeMissing_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN", []byte("found\n"), nil)
	notFound := []byte("could not be found")
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN", notFound, &exec.ExitError{})
	mgr := newTestManager(runner)

	status, err := mgr.Verify(context.Background())

	require.Error(t, err)
	assert.True(t, status["SLACK_BOT_TOKEN"])
	assert.False(t, status["SLACK_APP_TOKEN"])
	assert.Contains(t, err.Error(), "SLACK_APP_TOKEN")
}

func TestManager_List_ReturnsStatusMap(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security find-generic-password -s squad0 -a SLACK_BOT_TOKEN", []byte("found\n"), nil)
	notFound := []byte("could not be found")
	runner.On("security find-generic-password -s squad0 -a SLACK_APP_TOKEN", notFound, &exec.ExitError{})
	mgr := newTestManager(runner)

	status, err := mgr.List(context.Background())

	require.NoError(t, err)
	assert.True(t, status["SLACK_BOT_TOKEN"])
	assert.False(t, status["SLACK_APP_TOKEN"])
}

func TestManager_Set_ValidName_Succeeds(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("security add-generic-password -s squad0 -a SLACK_BOT_TOKEN -w test-value -U", nil, nil)
	mgr := newTestManager(runner)

	err := mgr.Set(context.Background(), "SLACK_BOT_TOKEN", "test-value")

	require.NoError(t, err)
}

func TestManager_Set_InvalidName_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	mgr := newTestManager(runner)

	err := mgr.Set(context.Background(), "INVALID_SECRET", "value")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognised secret name")
	assert.Contains(t, err.Error(), "INVALID_SECRET")
}
