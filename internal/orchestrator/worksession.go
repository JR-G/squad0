package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

const implementationPromptTemplate = `You are working on ticket %s.

## Task
%s

## Instructions
1. Read the ticket carefully and understand what needs to be done
2. Use recall to check your memory for anything relevant to this work
3. Explore the codebase to understand the existing code
4. Implement the changes with clean, well-tested code
5. Make atomic commits with conventional commit messages (feat:, fix:, etc.)
6. When finished, open a PR using: gh pr create --title "..." --body "..."
7. Move the ticket to "In Review" status using Linear MCP tools
8. Use remember_fact to store any important learnings from this work

Keep your implementation focused on the ticket scope. If you discover issues outside the scope, create new Linear tickets for them using the Linear MCP tools — don't expand the scope of your current work.

When you open the PR, include:
- A clear title referencing the ticket
- A description of what was changed and why
- Any testing notes
`

// BuildImplementationPrompt creates the prompt for an engineer session.
func BuildImplementationPrompt(ticket, description string) string {
	return fmt.Sprintf(implementationPromptTemplate, ticket, description)
}

// WorkSession manages the full lifecycle of an agent working on a ticket:
// create worktree → implement → open PR → clean up.
type WorkSession struct {
	repoDir     string
	worktreeDir string
}

// NewWorkSession creates a worktree for the agent to work in.
func NewWorkSession(ctx context.Context, repoDir string, role agent.Role, ticket string) (*WorkSession, error) {
	branch := fmt.Sprintf("feat/%s", strings.ToLower(ticket))
	worktreeDir := fmt.Sprintf("%s/.worktrees/%s", repoDir, role)

	output, err := gitCommand(ctx, repoDir, "worktree", "add", "-b", branch, worktreeDir)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %s: %w", string(output), err)
	}

	log.Printf("created worktree for %s at %s (branch %s)", role, worktreeDir, branch)

	return &WorkSession{
		repoDir:     repoDir,
		worktreeDir: worktreeDir,
	}, nil
}

// Dir returns the worktree directory path.
func (ws *WorkSession) Dir() string {
	return ws.worktreeDir
}

// Cleanup removes the worktree and prunes stale references.
func (ws *WorkSession) Cleanup(ctx context.Context) {
	logWorktreeRemoval(ctx, ws.repoDir, ws.worktreeDir)
	_, _ = gitCommand(ctx, ws.repoDir, "worktree", "prune")
}

func logWorktreeRemoval(ctx context.Context, repoDir, worktreeDir string) {
	output, err := gitCommand(ctx, repoDir, "worktree", "remove", "--force", worktreeDir)
	if err != nil {
		log.Printf("failed to remove worktree %s: %s: %v", worktreeDir, string(output), err)
		return
	}
	log.Printf("cleaned up worktree %s", worktreeDir)
}

func gitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
