package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteHookSettings_CreatesSettingsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := runtime.WriteHookSettings(dir, "engineer-1")
	require.NoError(t, err)

	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	assert.FileExists(t, settingsPath)
}

func TestWriteHookSettings_ContainsHooks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, runtime.WriteHookSettings(dir, "pm"))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings))

	hooks, ok := settings["hooks"].(map[string]interface{})
	require.True(t, ok, "settings should have hooks key")

	sessionStart, ok := hooks["SessionStart"]
	require.True(t, ok, "hooks should have SessionStart")
	assert.NotEmpty(t, sessionStart)

	userPrompt, ok := hooks["UserPromptSubmit"]
	require.True(t, ok, "hooks should have UserPromptSubmit")
	assert.NotEmpty(t, userPrompt)
}

func TestWriteHookSettings_CommandsContainRole(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, runtime.WriteHookSettings(dir, "engineer-2"))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "prime --role engineer-2")
	assert.Contains(t, content, "inbox drain --role engineer-2")
}
