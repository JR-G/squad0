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

func TestWriteHookSettings_InvalidDir_ReturnsError(t *testing.T) {
	t.Parallel()

	// Use a file path as the directory to trigger MkdirAll failure.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("block"), 0o644))

	err := runtime.WriteHookSettings(filePath, "pm")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating .claude dir")
}

func TestWriteHookSettings_OverwritesExistingSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write settings for one role, then overwrite with another.
	require.NoError(t, runtime.WriteHookSettings(dir, "engineer-1"))
	require.NoError(t, runtime.WriteHookSettings(dir, "tech-lead"))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "prime --role tech-lead")
	assert.NotContains(t, content, "prime --role engineer-1")
}

func TestWriteHookSettings_ValidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, runtime.WriteHookSettings(dir, "reviewer"))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Verify the full structure is correct JSON.
	hooks, ok := parsed["hooks"].(map[string]interface{})
	require.True(t, ok)

	sessionStart, ok := hooks["SessionStart"].([]interface{})
	require.True(t, ok)
	require.Len(t, sessionStart, 1)

	entry, ok := sessionStart[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "", entry["matcher"])

	innerHooks, ok := entry["hooks"].([]interface{})
	require.True(t, ok)
	require.Len(t, innerHooks, 1)

	hookCmd, ok := innerHooks[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "command", hookCmd["type"])
	assert.Contains(t, hookCmd["command"], "prime --role reviewer")
}

func TestWriteHookSettings_PreexistingClaudeDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Pre-create the .claude directory.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude"), 0o755))

	err := runtime.WriteHookSettings(dir, "designer")

	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, ".claude", "settings.json"))
}

func TestWriteHookSettings_SettingsFilePermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, runtime.WriteHookSettings(dir, "pm"))

	info, err := os.Stat(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	// File should be readable/writable by owner (0644).
	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o644), perm)
}
