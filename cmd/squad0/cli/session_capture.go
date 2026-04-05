package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// newSessionCommand creates the `squad0 session` subcommand group.
func newSessionCommand() *cobra.Command {
	sessionCmd := &cobra.Command{
		Use:    "session",
		Short:  "Persistent session management (hook handlers)",
		Hidden: true,
	}

	sessionCmd.AddCommand(newSessionCaptureCommand())
	return sessionCmd
}

// newSessionCaptureCommand creates `squad0 session capture`.
// Called by the Stop hook after Claude finishes responding.
// Reads transcript_path from stdin (JSON), extracts the last
// assistant response, and writes it to the outbox so Send returns.
func newSessionCaptureCommand() *cobra.Command {
	var role string
	var dataDir string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture session response to outbox (Stop hook handler)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionCapture(role, dataDir)
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "agent role")
	cmd.Flags().StringVar(&dataDir, "data-dir", "data", "path to data directory")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}

// stopHookInput is the JSON that Claude Code sends to the Stop hook via stdin.
type stopHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	StopHookActive bool   `json:"stop_hook_active"`
}

func runSessionCapture(role, dataDir string) error {
	// Read the hook input from stdin.
	var input stopHookInput
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		return fmt.Errorf("reading stop hook input: %w", err)
	}

	// Prevent infinite loops — if the stop hook is already active,
	// this is a re-entry. Let Claude stop.
	if input.StopHookActive {
		return nil
	}

	if input.TranscriptPath == "" {
		return nil
	}

	// Extract the last assistant response from the transcript.
	response := extractLastResponse(input.TranscriptPath)
	if response == "" {
		return nil
	}

	// Write to the outbox so Send can return.
	return writeToOutbox(role, dataDir, response)
}

// extractLastResponse reads the JSONL transcript and returns the last
// assistant text response.
func extractLastResponse(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	var lastResponse string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines

	for scanner.Scan() {
		line := scanner.Text()
		response := parseTranscriptLine(line)
		if response != "" {
			lastResponse = response
		}
	}

	return lastResponse
}

func parseTranscriptLine(line string) string {
	var entry struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Result  string          `json:"result"`
		Message json.RawMessage `json:"message"`
	}

	if json.Unmarshal([]byte(line), &entry) != nil {
		return ""
	}

	// "result" type has the final response.
	if entry.Type == "result" && entry.Result != "" {
		return entry.Result
	}

	// "assistant" type has content blocks.
	if entry.Type == "assistant" || entry.Role == "assistant" {
		return extractAssistantTextFromMessage(entry.Message)
	}

	return ""
}

func extractAssistantTextFromMessage(msg json.RawMessage) string {
	if msg == nil {
		return ""
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if json.Unmarshal(msg, &parsed) != nil {
		return ""
	}

	var builder strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			builder.WriteString(block.Text)
		}
	}

	return builder.String()
}

// writeToOutbox finds the most recent pending request ID in the outbox
// directory and writes the response to it.
func writeToOutbox(role, dataDir, response string) error {
	outboxDir := filepath.Join(dataDir, "outbox", role)
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		return fmt.Errorf("creating outbox dir: %w", err)
	}

	// Find pending request — the inbox enqueued a message with an ID,
	// and Send is waiting for {id}-response.json in the outbox.
	// We write a signal file that Send watches for.
	signalPath := filepath.Join(outboxDir, "latest-response.json")
	data, err := json.Marshal(map[string]string{
		"response": response,
	})
	if err != nil {
		return fmt.Errorf("marshalling response: %w", err)
	}

	return os.WriteFile(signalPath, data, 0o644)
}
