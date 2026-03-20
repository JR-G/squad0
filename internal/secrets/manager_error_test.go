package secrets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_LoadAll_NonNotFoundError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a SLACK_BOT_TOKEN -w",
		[]byte("some failure output"),
		fmt.Errorf("keychain access denied"),
	)
	mgr := newTestManager(runner)

	_, err := mgr.LoadAll(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading secret SLACK_BOT_TOKEN")
}

func TestManager_Verify_ExistsError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a SLACK_BOT_TOKEN",
		[]byte("unexpected failure"),
		fmt.Errorf("keychain access denied"),
	)
	mgr := newTestManager(runner)

	_, err := mgr.Verify(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking secret")
}

func TestManager_CheckPresence_ExistsError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On(
		"security find-generic-password -s squad0 -a SLACK_BOT_TOKEN",
		[]byte("failure"),
		fmt.Errorf("keychain locked"),
	)
	mgr := newTestManager(runner)

	_, err := mgr.List(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking secret SLACK_BOT_TOKEN")
}
