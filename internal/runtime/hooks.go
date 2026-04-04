package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// hookSettings is the .claude/settings.json structure for persistent
// sessions. Configures SessionStart and UserPromptSubmit hooks.
type hookSettings struct {
	Hooks hookConfig `json:"hooks"`
}

type hookConfig struct {
	SessionStart     []hookEntry `json:"SessionStart"`
	UserPromptSubmit []hookEntry `json:"UserPromptSubmit"`
}

type hookEntry struct {
	Matcher string `json:"matcher"`
	Command string `json:"command"`
}

// WriteHookSettings writes a .claude/settings.json to the working
// directory with hooks configured for the given agent role. The
// SessionStart hook injects personality via `squad0 prime`, and the
// UserPromptSubmit hook drains the inbox via `squad0 inbox drain`.
func WriteHookSettings(workDir, role string) error {
	squad0Bin := resolveSquad0Binary()

	settings := hookSettings{
		Hooks: hookConfig{
			SessionStart: []hookEntry{
				{
					Matcher: "",
					Command: fmt.Sprintf("%s prime --role %s", squad0Bin, role),
				},
			},
			UserPromptSubmit: []hookEntry{
				{
					Matcher: "",
					Command: fmt.Sprintf("%s inbox drain --role %s", squad0Bin, role),
				},
			},
		},
	}

	claudeDir := filepath.Join(workDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling hook settings: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	if writeErr := os.WriteFile(settingsPath, data, 0o644); writeErr != nil {
		return fmt.Errorf("writing hook settings: %w", writeErr)
	}

	return nil
}

func resolveSquad0Binary() string {
	exe, err := os.Executable()
	if err != nil {
		return "squad0"
	}
	return exe
}
