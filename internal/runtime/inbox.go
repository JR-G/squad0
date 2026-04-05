package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// InboxMessage is a prompt delivered to an agent via the filesystem queue.
type InboxMessage struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Timestamp time.Time `json:"timestamp"`
}

// OutboxMessage is a response written by the agent to the outbox.
type OutboxMessage struct {
	ID        string    `json:"id"`
	Response  string    `json:"response"`
	Timestamp time.Time `json:"timestamp"`
}

// Inbox manages a filesystem-based message queue. Messages are JSON
// files in a directory, claimed atomically via rename.
type Inbox struct {
	inboxDir  string
	outboxDir string
}

// NewInbox creates an Inbox with the given directories. Creates the
// directories if they don't exist.
func NewInbox(inboxDir, outboxDir string) (*Inbox, error) {
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating inbox dir: %w", err)
	}
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating outbox dir: %w", err)
	}
	return &Inbox{inboxDir: inboxDir, outboxDir: outboxDir}, nil
}

// Enqueue writes a prompt to the inbox as a JSON file. Atomic: writes
// to a .tmp file first, then renames to .json.
func (inbox *Inbox) Enqueue(prompt string) (string, error) {
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	msg := InboxMessage{
		ID:        id,
		Prompt:    prompt,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("marshalling inbox message: %w", err)
	}

	tmpPath := filepath.Join(inbox.inboxDir, id+".tmp")
	finalPath := filepath.Join(inbox.inboxDir, id+".json")

	if writeErr := os.WriteFile(tmpPath, data, 0o644); writeErr != nil {
		return "", fmt.Errorf("writing inbox tmp file: %w", writeErr)
	}

	if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("renaming inbox file: %w", renameErr)
	}

	return id, nil
}

// Drain reads all pending messages from the inbox, claims them
// atomically (rename to .claimed), and returns them sorted by
// timestamp. Called by the UserPromptSubmit hook.
func (inbox *Inbox) Drain() ([]InboxMessage, error) {
	entries, err := os.ReadDir(inbox.inboxDir)
	if err != nil {
		return nil, fmt.Errorf("reading inbox dir: %w", err)
	}

	messages := make([]InboxMessage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		msg, claimErr := inbox.claimAndRead(entry.Name())
		if claimErr != nil {
			continue // Another process claimed it — skip.
		}
		messages = append(messages, msg)
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}

func (inbox *Inbox) claimAndRead(filename string) (InboxMessage, error) {
	jsonPath := filepath.Join(inbox.inboxDir, filename)
	claimedPath := jsonPath + ".claimed"

	// Atomic claim via rename.
	if err := os.Rename(jsonPath, claimedPath); err != nil {
		return InboxMessage{}, fmt.Errorf("claiming %s: %w", filename, err)
	}

	data, err := os.ReadFile(claimedPath)
	if err != nil {
		return InboxMessage{}, fmt.Errorf("reading claimed %s: %w", filename, err)
	}

	// Clean up claimed file after reading.
	_ = os.Remove(claimedPath)

	var msg InboxMessage
	if jsonErr := json.Unmarshal(data, &msg); jsonErr != nil {
		return InboxMessage{}, fmt.Errorf("parsing %s: %w", filename, jsonErr)
	}

	return msg, nil
}

// WaitForResponse polls the outbox for a response with the given ID.
// Returns the response text or an error if the timeout is reached.
func (inbox *Inbox) WaitForResponse(id string, timeout time.Duration) (string, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	responsePath := filepath.Join(inbox.outboxDir, id+"-response.json")

	for {
		select {
		case <-deadline:
			return "", fmt.Errorf("timeout waiting for response %s after %s", id, timeout)
		case <-ticker.C:
			data, err := os.ReadFile(responsePath)
			if err != nil {
				continue // Not yet — keep polling.
			}

			// Clean up response file.
			_ = os.Remove(responsePath)

			var msg OutboxMessage
			if jsonErr := json.Unmarshal(data, &msg); jsonErr != nil {
				return "", fmt.Errorf("parsing response %s: %w", id, jsonErr)
			}
			return msg.Response, nil
		}
	}
}

// WriteResponse writes a response to the outbox. Called by the agent
// (or the hook system) after generating a response.
func (inbox *Inbox) WriteResponse(id, response string) error {
	msg := OutboxMessage{
		ID:        id,
		Response:  response,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshalling outbox message: %w", err)
	}

	path := filepath.Join(inbox.outboxDir, id+"-response.json")
	return os.WriteFile(path, data, 0o644)
}

// FormatDrained formats drained messages as system-reminder blocks
// for injection into a Claude Code session via hook stdout.
func FormatDrained(messages []InboxMessage) string {
	if len(messages) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, msg := range messages {
		builder.WriteString("<system-reminder>\n")
		builder.WriteString(msg.Prompt)
		builder.WriteString("\n</system-reminder>\n")
	}
	return builder.String()
}

// WaitForSignal watches for the latest-response.json signal file
// in the outbox. Uses a fast poll (50ms) since the Stop hook writes
// the file immediately after Claude responds — the wait is typically
// <100ms, not seconds.
func (inbox *Inbox) WaitForSignal(ctx context.Context) (string, error) {
	signalPath := filepath.Join(inbox.outboxDir, "latest-response.json")

	// Remove any stale signal from a previous turn.
	_ = os.Remove(signalPath)

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			data, err := os.ReadFile(signalPath)
			if err != nil {
				continue
			}

			_ = os.Remove(signalPath)

			var parsed struct {
				Response string `json:"response"`
			}
			if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
				return "", fmt.Errorf("parsing signal response: %w", jsonErr)
			}
			return parsed.Response, nil
		}
	}
}

// InboxDir returns the inbox directory path.
func (inbox *Inbox) InboxDir() string {
	return inbox.inboxDir
}

// OutboxDir returns the outbox directory path.
func (inbox *Inbox) OutboxDir() string {
	return inbox.outboxDir
}
