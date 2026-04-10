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

## Summary
%s

## Workflow
Step 1: CONTEXT — Read the full ticket from Linear using your MCP tools. Use recall to check memory for anything relevant.
Step 2: EXPLORE — Read the codebase to understand the existing code and patterns.
Step 3: IMPLEMENT — Write clean, well-tested code. Make atomic commits (feat:, fix:, etc.).
Step 4: SELF-REVIEW — Read your own diff. Check for missing tests, error handling, edge cases.
Step 5: VERIFY — Run the test suite. Fix any failures.
Step 6: SUBMIT — Push and create a PR:
   git push -u origin HEAD
   gh pr create --title "%s: <description>" --body "<what and why>"
Step 7: DONE — Move ticket to "In Review" status.

Your session is not complete until a PR exists on GitHub. If a step fails, fix it and continue.
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
		cleanupStaleWorktree(ctx, repoDir, worktreeDir)

		session, err := createWorktree(ctx, repoDir, worktreeDir, branch, role)
		if err == nil {
			return session, nil
		}

		lastErr = fmt.Errorf("creating worktree (attempt %d): %w", attempt+1, err)
		log.Printf("worktree attempt %d failed for %s: %v", attempt+1, role, lastErr)

		forceCleanup(ctx, repoDir, worktreeDir)
	}

	return nil, lastErr
}

// NewFixUpSession creates a worktree from an existing PR branch so
// fix-up sessions work in isolation on the correct branch. The agent
// pushes to the same branch — no new PRs created.
func NewFixUpSession(ctx context.Context, repoDir, prURL string, role agent.Role, ticket string) (*WorkSession, error) {
	worktreeDir := fmt.Sprintf("%s/.worktrees/%s-fixup", repoDir, role)
	branch := extractPRBranch(ctx, repoDir, prURL)
	if branch == "" {
		branch = fmt.Sprintf("feat/%s", strings.ToLower(ticket))
	}

	// Fetch the branch from origin so the worktree has it.
	_, _ = gitCommand(ctx, repoDir, "fetch", "origin", branch)

	// Clean any stale worktree for this role.
	_ = os.RemoveAll(worktreeDir)
	_, _ = gitCommand(ctx, repoDir, "worktree", "prune")

	// Check out the existing branch — no -b flag.
	output, err := gitCommand(ctx, repoDir, "worktree", "add", worktreeDir, branch)
	if err != nil {
		return nil, fmt.Errorf("creating fix-up worktree for %s on %s: %s: %w", role, branch, string(output), err)
	}

	log.Printf("created fix-up worktree for %s at %s (branch %s)", role, worktreeDir, branch)
	return &WorkSession{repoDir: repoDir, worktreeDir: worktreeDir}, nil
}

// extractPRBranch uses gh to get the head branch of a PR.
func extractPRBranch(ctx context.Context, repoDir, prURL string) string {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prURL, "--json", "headRefName", "--jq", ".headRefName")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
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

func createWorktree(ctx context.Context, repoDir, worktreeDir, branch string, role agent.Role) (*WorkSession, error) {
	// New branch — first attempt for fresh tickets.
	output, err := gitCommand(ctx, repoDir, "worktree", "add", "-b", branch, worktreeDir)
	if err == nil {
		log.Printf("created worktree for %s at %s (branch %s)", role, worktreeDir, branch)
		return &WorkSession{repoDir: repoDir, worktreeDir: worktreeDir}, nil
	}

	// Branch already exists (e.g. open PR) — check it out instead.
	if !branchExists(ctx, repoDir, branch) {
		return nil, fmt.Errorf("%s: %w", string(output), err)
	}

	output, err = gitCommand(ctx, repoDir, "worktree", "add", worktreeDir, branch)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", string(output), err)
	}

	log.Printf("created worktree for %s at %s (existing branch %s)", role, worktreeDir, branch)
	return &WorkSession{repoDir: repoDir, worktreeDir: worktreeDir}, nil
}

// cleanupStaleWorktree removes the worktree directory and prunes git
// state. Also detaches the main repo if it's on a feature branch —
// worktrees can't check out a branch that's already checked out.
func cleanupStaleWorktree(ctx context.Context, repoDir, worktreeDir string) {
	_, _ = gitCommand(ctx, repoDir, "worktree", "remove", "--force", worktreeDir)
	_ = os.RemoveAll(worktreeDir)
	_, _ = gitCommand(ctx, repoDir, "worktree", "prune")
	detachIfOnFeatureBranch(ctx, repoDir)
}

// forceCleanup is a last-resort cleanup that removes the worktree
// directory from disk. Does NOT delete branches — they may belong
// to open PRs.
func forceCleanup(ctx context.Context, repoDir, worktreeDir string) {
	_ = os.RemoveAll(worktreeDir)
	_, _ = gitCommand(ctx, repoDir, "worktree", "prune")
	detachIfOnFeatureBranch(ctx, repoDir)
}

// detachIfOnFeatureBranch switches the main repo to main if it's
// stuck on a feature branch. Git refuses to create a worktree for
// a branch that's already checked out elsewhere.
func detachIfOnFeatureBranch(ctx context.Context, repoDir string) {
	output, err := gitCommand(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return
	}
	branch := strings.TrimSpace(string(output))
	if branch == "main" || branch == "HEAD" {
		return
	}
	log.Printf("worktree: main repo is on %s — switching to main", branch)
	_, _ = gitCommand(ctx, repoDir, "checkout", "main")
}

// branchExists returns true if a local branch with the given name exists.
func branchExists(ctx context.Context, repoDir, branch string) bool {
	_, err := gitCommand(ctx, repoDir, "rev-parse", "--verify", branch)
	return err == nil
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
