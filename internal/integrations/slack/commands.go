package slack

import (
	"fmt"
	"strings"
)

// Command represents a parsed CEO command from the #commands channel.
type Command struct {
	Name string
	Args []string
}

// CommandType enumerates the recognised CEO commands.
type CommandType string

const (
	// CommandStart starts the orchestrator loop.
	CommandStart CommandType = "start"
	// CommandStop gracefully stops all agents.
	CommandStop CommandType = "stop"
	// CommandStatus shows all agent statuses.
	CommandStatus CommandType = "status"
	// CommandStandup triggers a manual standup.
	CommandStandup CommandType = "standup"
	// CommandRetro triggers a manual retro.
	CommandRetro CommandType = "retro"
	// CommandAssign manually assigns a ticket to an agent.
	CommandAssign CommandType = "assign"
	// CommandPause pauses an agent or all agents.
	CommandPause CommandType = "pause"
	// CommandResume resumes an agent or all agents.
	CommandResume CommandType = "resume"
	// CommandDiscuss triggers a design discussion.
	CommandDiscuss CommandType = "discuss"
	// CommandAgents lists all agents with models and status.
	CommandAgents CommandType = "agents"
	// CommandMemory shows an agent's top beliefs.
	CommandMemory CommandType = "memory"
	// CommandFeed posts a real-time activity summary.
	CommandFeed CommandType = "feed"
	// CommandProblems shows agents needing intervention.
	CommandProblems CommandType = "problems"
	// CommandHealth shows agent health states.
	CommandHealth CommandType = "health"
	// CommandMergeMode sets the merge autonomy mode.
	CommandMergeMode CommandType = "merge-mode"
	// CommandVersion shows the version.
	CommandVersion CommandType = "version"
)

// ValidCommands returns all recognised command names.
func ValidCommands() []CommandType {
	return []CommandType{
		CommandStart, CommandStop, CommandStatus, CommandStandup,
		CommandRetro, CommandAssign, CommandPause, CommandResume,
		CommandDiscuss, CommandAgents, CommandMemory, CommandFeed,
		CommandProblems, CommandHealth, CommandMergeMode, CommandVersion,
	}
}

// ParseCommand parses a plain text message from the #commands channel
// into a Command. Returns an error if the command is not recognised.
func ParseCommand(text string) (Command, error) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return Command{}, fmt.Errorf("empty command")
	}

	name := strings.ToLower(fields[0])

	if !isValidCommand(name) {
		return Command{}, fmt.Errorf("unrecognised command %q; valid commands: %s",
			name, validCommandList())
	}

	var args []string
	if len(fields) > 1 {
		args = fields[1:]
	}

	return Command{Name: name, Args: args}, nil
}

func isValidCommand(name string) bool {
	for _, cmd := range ValidCommands() {
		if string(cmd) == name {
			return true
		}
	}
	return false
}

func validCommandList() string {
	commands := ValidCommands()
	names := make([]string, 0, len(commands))
	for _, cmd := range commands {
		names = append(names, string(cmd))
	}
	return strings.Join(names, ", ")
}
