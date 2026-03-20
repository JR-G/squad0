package main_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain_Binary_VersionCommand(t *testing.T) {
	t.Parallel()

	binary := buildTestBinary(t)
	output, err := exec.Command(binary, "version").CombinedOutput()

	require.NoError(t, err)
	assert.Contains(t, string(output), "squad0 version")
}

func TestMain_Binary_NoArgs_PrintsUsage(t *testing.T) {
	t.Parallel()

	binary := buildTestBinary(t)
	output, err := exec.Command(binary, "--help").CombinedOutput()

	require.NoError(t, err)
	assert.Contains(t, string(output), "squad0")
}

func buildTestBinary(t *testing.T) string {
	t.Helper()

	binary := t.TempDir() + "/squad0"
	cmd := exec.Command("go", "build", "-tags", "sqlite_fts5", "-o", binary, ".")
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(output))

	return binary
}
