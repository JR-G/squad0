package orchestrator

import (
	"context"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

const preSubmitPrompt = `Verify your work is clean before we submit for review:
1. Run: git status — are there uncommitted changes? If so, commit them.
2. Run: git diff origin/main..HEAD --stat — confirm your changes are on the branch.
3. Run the project's test command if one exists.
Push any remaining commits.
Respond with "CLEAN" if everything is good, or describe what's wrong.`

// RunPreSubmitCheck runs a verification session with the engineer to
// ensure the work is clean before submitting for review. Returns true
// if the check passed.
func RunPreSubmitCheck(ctx context.Context, engineerAgent *agent.Agent, workDir string) bool {
	result, err := engineerAgent.DirectSession(ctx, preSubmitPrompt)
	if err != nil {
		log.Printf("pre-submit check failed for %s: %v", engineerAgent.Role(), err)
		return false
	}

	clean := strings.Contains(strings.ToUpper(result.Transcript), "CLEAN")
	if !clean {
		log.Printf("pre-submit check: %s reported issues: %s",
			engineerAgent.Role(), agent.TruncateSummary(result.Transcript, 200))
	}

	return clean
}

// PreSubmitCheckForTest exports the pre-submit prompt for testing.
func PreSubmitCheckForTest() string {
	return preSubmitPrompt
}
