package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPrime_WithTemporaryAgentsDir_WritesPersonality(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })

	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "agents"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "agents", "pm.md"),
		[]byte("# PM\n\n## Voice\n\nDirect and decisive.\n\n## How You Work\n\n- Keep scope tight.\n"),
		0o644,
	))
	require.NoError(t, os.Chdir(tmpDir))

	oldStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writePipe
	t.Cleanup(func() { os.Stdout = oldStdout })

	err = runPrime(context.Background(), "pm")
	require.NoError(t, err)
	require.NoError(t, writePipe.Close())

	var output bytes.Buffer
	_, err = output.ReadFrom(readPipe)
	require.NoError(t, err)

	text := output.String()
	assert.Contains(t, text, "# You are pm")
	assert.Contains(t, text, "## Full Personality")
	assert.Contains(t, text, "Direct and decisive.")
}

func TestResolveMemoryBinaryPath_ReturnsSiblingBinaryWhenPresent(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	candidate := filepath.Join(filepath.Dir(exe), "squad0-memory-mcp")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755))
	t.Cleanup(func() { _ = os.Remove(candidate) })

	assert.Equal(t, candidate, resolveMemoryBinaryPath())
}

func TestShowSecretStatus_PrintsConfiguredSecrets(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "security")
	script := `#!/bin/sh
key=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-a" ]; then
    key="$2"
    shift 2
    continue
  fi
  shift
done
if [ "$key" = "SLACK_BOT_TOKEN" ]; then
  exit 0
fi
if [ "$key" = "SLACK_APP_TOKEN" ]; then
  echo "not found" >&2
  exit 44
fi
exit 1
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	showSecretStatus(context.Background(), cmd)

	text := output.String()
	assert.Contains(t, text, "SLACK_BOT_TOKEN")
	assert.Contains(t, text, "SLACK_APP_TOKEN")
	assert.Contains(t, text, "set")
	assert.Contains(t, text, "not set")
}
