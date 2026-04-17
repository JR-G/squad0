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
Step 2: EXPLORE — Read the codebase to understand the existing code and patterns. Use working_set to capture intermediate findings (e.g. "files I've identified as relevant", "patterns I've spotted") so you can refer back to them later in this session without re-reading.
Step 3: PLAN — Before writing any code, write a 3-5 line plan in your output explaining:
   - the files you'll touch and why
   - the tests you'll add
   - what you're explicitly NOT doing (out of scope)
Step 4: CRITIQUE — Read your own plan and list 2-3 risks or weaknesses (e.g. edge cases you haven't covered, assumptions that might be wrong, tests that will be flaky). If any feel real, revise the plan before continuing.
Step 5: IMPLEMENT — Write clean, well-tested code. Make atomic commits (feat:, fix:, etc.).
Step 6: SELF-REVIEW — Read your own diff. Check for missing tests, error handling, edge cases. Address anything from Step 4 that you handwaved.
Step 7: VERIFY — Run the test suite. Fix any failures.
Step 8: SUBMIT — Push and create a PR:
   git push -u origin HEAD
   gh pr create --title "%s: <description>" --body "<what and why>"
Step 9: DONE — Move ticket to "In Review" status.

Steps 3 and 4 are not optional — write them out as plain prose before
starting the implementation. The point is to think before acting, not
to follow a checklist after the fact. If your plan changes during
implementation, note the change in your output before continuing.

## Working memory tools (session-scoped)
- working_set(key, value): jot down something you want to remember later in this session
- working_get(key): read it back
- working_keys(): list everything you've jotted down

These are cleared at session end. For permanent storage use remember_fact / store_belief instead.

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

	// Check out the existing branch. --force lets us create the
	// worktree even when the branch is already checked out in the
	// main repo or another worktree — the fix-up worktree is
	// ephemeral so parallel use is safe.
	output, err := gitCommand(ctx, repoDir, "worktree", "add", "--force", worktreeDir, branch)
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

// Cleanup removes the worktree and prunes stale references. Both
// errors are logged but not returned — Cleanup is called from defer
// and there's nothing the caller can do — but a final unrecoverable
// state is logged at WARN so leaks surface in the console.
func (ws *WorkSession) Cleanup(ctx context.Context) {
	logWorktreeRemoval(ctx, ws.repoDir, ws.worktreeDir)
	if pruneOut, pruneErr := gitCommand(ctx, ws.repoDir, "worktree", "prune"); pruneErr != nil {
		log.Printf("WARN: worktree prune after cleanup of %s failed: %s: %v", ws.worktreeDir, strings.TrimSpace(string(pruneOut)), pruneErr)
	}
}

func logWorktreeRemoval(ctx context.Context, repoDir, worktreeDir string) {
	output, err := gitCommand(ctx, repoDir, "worktree", "remove", "--force", worktreeDir)
	if err == nil {
		log.Printf("cleaned up worktree %s", worktreeDir)
		return
	}

	// "not a working tree" and "No such file or directory" mean the
	// worktree is already gone — the cleanup ran twice, or someone
	// pruned it first. That's the desired end state, not a failure.
	// Prune stale refs so the next create doesn't trip over the
	// ghost; if even prune fails, log loudly because a stale ref
	// will trip the next agent into a worktree-locked error.
	if isAlreadyCleanWorktreeError(string(output)) {
		pruneStaleWorktreeRef(ctx, repoDir, worktreeDir)
		return
	}

	log.Printf("WARN: failed to remove worktree %s: %s: %v", worktreeDir, string(output), err)
}

func pruneStaleWorktreeRef(ctx context.Context, repoDir, worktreeDir string) {
	pruneOut, pruneErr := gitCommand(ctx, repoDir, "worktree", "prune")
	if pruneErr == nil {
		return
	}
	log.Printf("WARN: worktree prune of stale ref %s failed: %s: %v", worktreeDir, strings.TrimSpace(string(pruneOut)), pruneErr)
}

func isAlreadyCleanWorktreeError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "is not a working tree") ||
		strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "not a valid path")
}

func createWorktree(ctx context.Context, repoDir, worktreeDir, branch string, role agent.Role) (*WorkSession, error) {
	// New branch — first attempt for fresh tickets.
	output, err := gitCommand(ctx, repoDir, "worktree", "add", "-b", branch, worktreeDir)
	if err == nil {
		log.Printf("created worktree for %s at %s (branch %s)", role, worktreeDir, branch)
		return &WorkSession{repoDir: repoDir, worktreeDir: worktreeDir}, nil
	}

	// If git refuses because the directory is already in use by
	// another worktree, do NOT retry with --force. That would race
	// a live agent into the same tree. Bail with a clear error so
	// the orchestrator can pick a different role or wait.
	if isWorktreeLockedOutput(string(output)) {
		return nil, fmt.Errorf("worktree locked for %s at %s: %s", role, worktreeDir, strings.TrimSpace(string(output)))
	}

	// Branch already exists (e.g. open PR) — check it out instead.
	if !branchExists(ctx, repoDir, branch) {
		return nil, fmt.Errorf("%s: %w", string(output), err)
	}

	output, err = gitCommand(ctx, repoDir, "worktree", "add", "--force", worktreeDir, branch)
	if err == nil {
		log.Printf("created worktree for %s at %s (existing branch %s)", role, worktreeDir, branch)
		return &WorkSession{repoDir: repoDir, worktreeDir: worktreeDir}, nil
	}
	if isWorktreeLockedOutput(string(output)) {
		return nil, fmt.Errorf("worktree locked for %s at %s: %s", role, worktreeDir, strings.TrimSpace(string(output)))
	}
	return nil, fmt.Errorf("%s: %w", string(output), err)
}

func isWorktreeLockedOutput(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "is already used by worktree") ||
		strings.Contains(lower, "already locked") ||
		strings.Contains(lower, "is already checked out at")
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

// IsAlreadyCleanWorktreeErrorForTest exports isAlreadyCleanWorktreeError for testing.
func IsAlreadyCleanWorktreeErrorForTest(output string) bool {
	return isAlreadyCleanWorktreeError(output)
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
