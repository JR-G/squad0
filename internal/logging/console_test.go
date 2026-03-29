package logging_test

import (
	"bytes"
	"testing"

	"github.com/JR-G/squad0/internal/logging"
	"github.com/stretchr/testify/assert"
)

func TestConsoleWriter_FormatsOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("2026/03/27 15:07:33 tick: work_enabled=true"))
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "tick")
	assert.Contains(t, output, "work_enabled=true")
	assert.NotEmpty(t, output)
}

func TestConsoleWriter_EmptyInput_NoOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("   "))
	assert.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestConsoleWriter_ErrorCategory_Coloured(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("session error for engineer-1: timeout"))
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "error")
}

func TestConsoleWriter_MergeCategory(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("merge: JAM-7 has conflicts"))
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "merge")
	assert.Contains(t, output, "JAM-7")
}

func TestConsoleWriter_ChatCategory(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("chat: engineer-2 responding..."))
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "chat")
}

func TestConsoleWriter_HighlightsRoles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := logging.NewConsoleWriter(&buf)

	_, err := writer.Write([]byte("idle duty: tech-lead reviewed engineer-2's PR"))
	assert.NoError(t, err)

	output := buf.String()
	// Role names should be bolded (wrapped in ANSI codes).
	assert.Contains(t, output, "tech-lead")
	assert.Contains(t, output, "engineer-2")
}
