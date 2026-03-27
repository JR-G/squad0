package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
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
6. Push your branch and open a PR — this is MANDATORY, do not skip:
   git push -u origin HEAD
   gh pr create --title "%s: <description>" --body "<what and why>"
7. Move the ticket to "In Review" status using Linear MCP tools
8. Use remember_fact to store any important learnings from this work

CRITICAL: Your session is not complete until a PR exists on GitHub. After implementing, you MUST push and create a PR. If gh pr create fails, fix the issue and retry.

Keep your implementation focused on the ticket scope. If you discover issues outside the scope, create new Linear tickets for them — don't expand the scope of your current work.
`

const maxWorktreeRetries = 3

// BuildImplementationPrompt creates the prompt for an engineer session.
func BuildImplementationPrompt(ticket, description string) string {
	return fmt.Sprintf(implementationPromptTemplate, ticket, description, ticket)
}

// WorkSession manages the full lifecycle of an agent working on a ticket:
// create worktree → implement → open PR → clean up.
type WorkSession struct {
	repoDir     string
	worktreeDir string
}

// NewWorkSession creates a worktree for the agent to work in. Retries
// up to 3 times with progressively more aggressive cleanup between
// attempts so agents can self-heal from stale state.
func NewWorkSession(ctx context.Context, repoDir string, role agent.Role, ticket string) (*WorkSession, error) {
	branch := fmt.Sprintf("feat/%s", strings.ToLower(ticket))
	worktreeDir := fmt.Sprintf("%s/.worktrees/%s", repoDir, role)

	var lastErr error
	for attempt := range maxWorktreeRetries {
		cleanupStaleWorktree(ctx, repoDir, worktreeDir, branch)

		output, err := gitCommand(ctx, repoDir, "worktree", "add", "-b", branch, worktreeDir)
		if err == nil {
			log.Printf("created worktree for %s at %s (branch %s)", role, worktreeDir, branch)
			return &WorkSession{repoDir: repoDir, worktreeDir: worktreeDir}, nil
		}

		lastErr = fmt.Errorf("creating worktree (attempt %d): %s: %w", attempt+1, string(output), err)
		log.Printf("worktree attempt %d failed for %s: %v", attempt+1, role, lastErr)

		// More aggressive cleanup on retry: remove the directory manually
		// and prune in case git's internal state is stale.
		forceCleanup(ctx, repoDir, worktreeDir, branch)
	}

	return nil, lastErr
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

func cleanupStaleWorktree(ctx context.Context, repoDir, worktreeDir, branch string) {
	_, _ = gitCommand(ctx, repoDir, "worktree", "remove", "--force", worktreeDir)
	_, _ = gitCommand(ctx, repoDir, "branch", "-D", branch)
	_, _ = gitCommand(ctx, repoDir, "worktree", "prune")
}

// forceCleanup is a last-resort cleanup that removes the worktree
// directory from disk and deletes the branch, ignoring all errors.
// Used when git's internal state has diverged from what's on disk.
func forceCleanup(ctx context.Context, repoDir, worktreeDir, branch string) {
	_ = os.RemoveAll(worktreeDir)
	_, _ = gitCommand(ctx, repoDir, "worktree", "prune")
	_, _ = gitCommand(ctx, repoDir, "branch", "-D", branch)
}

func gitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// RescuePRForTest exports rescuePR for testing.
func (orch *Orchestrator) RescuePRForTest(ctx context.Context, agentInstance *agent.Agent, workDir, ticket, branch string) string {
	return orch.rescuePR(ctx, agentInstance, workDir, ticket, branch)
}

// rescuePR attempts to push the branch and create a PR when the main
// session completed without one. Returns the PR URL if successful.
func (orch *Orchestrator) rescuePR(ctx context.Context, agentInstance *agent.Agent, workDir, ticket, branch string) string {
	prompt := fmt.Sprintf(
		"The work for %s is done but no PR was created. Do these steps NOW:\n"+
			"1. Run: git -C %s push -u origin %s\n"+
			"2. Run: gh pr create --title \"%s: implementation\" --body \"Automated PR for %s\" --head %s\n"+
			"3. Respond with ONLY the PR URL (e.g. https://github.com/owner/repo/pull/123) or 'FAILED' if it didn't work.",
		ticket, workDir, branch, ticket, ticket, branch)

	result, err := agentInstance.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("rescue PR failed for %s: %v", ticket, err)
		return ""
	}

	return ExtractPRURL(result.Transcript)
}
