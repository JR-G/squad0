package orchestrator

import "fmt"

const reviewPromptTemplate = `You are reviewing pull request #%s for ticket %s.

## PR
%s

## Instructions
1. Read the PR diff: gh pr diff %s
2. Read the PR description: gh pr view %s
   - Look for a '## Decisions Honoured' section in the body. Each bullet there is a DECISION the team made during the pre-implementation discussion that the engineer committed to honour.
   - For each listed decision, verify the code actually implements it. A decision that was ignored, substituted, or silently changed is a [blocker].
3. Check for review comments (CodeRabbit, humans, prior reviews): gh pr view %s --comments
4. Analyse the changes for:
   - Correctness: does the code do what the ticket asks?
   - Decisions: are all entries in 'Decisions Honoured' actually honoured in the diff?
   - Bugs: off-by-one errors, nil pointer dereferences, race conditions
   - Tests: are the changes adequately tested?
   - Security: any injection, XSS, or auth issues?

5. Post your findings as a PR comment. BUDGET: maximum 5 items. Prioritise ruthlessly.
   Label each item as [blocker] or [suggestion]:
   - [blocker]: bugs, security issues, missing error handling, broken tests. These PREVENT approval.
   - [suggestion]: style, naming, minor improvements. These do NOT prevent approval.
   gh pr comment %s --body 'your review with labelled items'

6. Make your decision:
   - If there are ZERO blockers: approve, even if you have suggestions.
   - If there are blockers: request changes.
   gh pr review %s --approve --body "summary" OR gh pr review %s --request-changes --body "summary"

   IMPORTANT: Do NOT request changes for suggestions only. Approve with suggestions noted.
   You MUST actually run the gh pr review command.

7. Verify: gh pr view %s --json reviewDecision

End your response with either APPROVED or CHANGES_REQUESTED on its own line.
`

const reReviewPromptTemplate = `You previously reviewed PR #%s for ticket %s and requested changes.
The engineer has pushed fixes.

## PR
%s

## Instructions
1. Read your previous review comments: gh pr view %s --comments
2. Read the latest diff: gh pr diff %s
3. ONLY check your previous [blocker] items. For each one:
   - Was the blocker resolved?
   - Did the fix introduce a NEW bug? (only flag if it's a genuine bug, not a style preference)
   Ignore suggestions and style — those are non-blocking.

4. Post a brief update: gh pr comment %s --body 'Re-review: [status of each blocker]'

5. Decision:
   - If all blockers are resolved: approve. Do not find new issues on re-review.
   - If a blocker is still broken: request changes for that specific item only.
   gh pr review %s --approve --body "Blockers addressed" OR gh pr review %s --request-changes --body "remaining blocker"

6. Verify: gh pr view %s --json reviewDecision

IMPORTANT: This is a re-review. Your job is to verify previous blockers are fixed.
Do NOT raise new issues. Do NOT re-review the entire diff. Keep it focused.
End with APPROVED or CHANGES_REQUESTED.
`

const fixUpPromptTemplate = `You need to address review feedback on your PR for ticket %s.

## PR
%s

## Instructions
1. Read ALL review comments on the PR: gh pr view %s --comments
2. Read the current diff: gh pr diff %s
3. Check CI status: gh pr checks %s
4. For EACH comment, address it specifically. Don't skip any.
5. Fix the code, update tests, handle edge cases
6. If the branch is behind main, rebase: git fetch origin main && git rebase origin/main
7. Commit your fixes with conventional commit messages
8. Push: git push
9. Verify CI passes after pushing: gh pr checks %s
10. After fixing, reply to the review thread: gh pr comment %s --body 'Addressed all feedback: [brief summary of what you fixed]'

Focus on what the reviewer asked for. Don't refactor unrelated code.
`

// BuildReviewPrompt creates the prompt for a reviewer session.
func BuildReviewPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(reviewPromptTemplate,
		prNum, ticket, prURL,
		prURL, prURL, prURL, prURL, prURL, prURL, prURL)
}

// BuildReReviewPrompt creates the prompt for re-reviewing after fixes.
func BuildReReviewPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(reReviewPromptTemplate,
		prNum, ticket, prURL,
		prURL, prURL, prURL, prURL, prURL, prURL)
}

// BuildFixUpPrompt creates the prompt for an engineer to address review feedback.
func BuildFixUpPrompt(prURL, ticket string) string {
	return fmt.Sprintf(fixUpPromptTemplate,
		ticket, prURL,
		prURL, prURL, prURL, prURL, prURL)
}

const engineerMergePromptTemplate = `Your PR for ticket %s has been approved. Finish it up and merge.

## PR
%s

## Instructions
1. Read any remaining review comments: gh pr view %s --comments
2. Address any minor comments if needed (small fixes only — the PR is approved)
3. Rebase if behind main: git fetch origin main && git rebase origin/main
4. Check CI: gh pr checks %s
5. Merge: gh pr merge %s --squash --delete-branch
6. Verify merged: gh pr view %s --json state --jq .state

If the merge fails due to conflicts, rebase and try again.
If CI is failing, fix and push before merging.
`

// BuildEngineerMergePrompt creates the prompt for an engineer to merge
// their own approved PR.
func BuildEngineerMergePrompt(prURL, ticket string) string {
	return fmt.Sprintf(engineerMergePromptTemplate,
		ticket, prURL,
		prURL, prURL, prURL, prURL)
}
