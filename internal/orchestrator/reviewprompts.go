package orchestrator

import "fmt"

const reviewPromptTemplate = `You are reviewing pull request #%s for ticket %s.

## PR
%s

## Instructions
1. Read the PR diff: gh pr diff %s
2. Read the PR description: gh pr view %s
3. Check for review comments (CodeRabbit, humans, prior reviews): gh pr view %s --comments
4. Analyse the changes for:
   - Correctness: does the code do what the ticket asks?
   - Bugs: off-by-one errors, nil pointer dereferences, race conditions
   - Tests: are the changes adequately tested?
   - Style: does it follow the project's conventions?
   - Security: any injection, XSS, or auth issues?

5. Post your detailed findings as a PR comment so the engineer can see them inline:
   gh pr comment %s --body 'your full review with numbered items for each issue'
   Each issue should be a numbered item. Be specific about file, line, and what to fix.

6. You MUST submit your review using one of these gh commands — this is MANDATORY:
   To approve: gh pr review %s --approve --body "short summary of your review"
   To request changes: gh pr review %s --request-changes --body "short summary of issues"

   The --body here is a SHORT summary only — the detail is in the PR comment above.

   IMPORTANT RULES:
   - Do NOT say "approved with comments" — that is NOT an approval.
   - If you have ANY concerns, use --request-changes. Don't be afraid to request changes.
   - The pipeline handles the fix-up loop automatically — requesting changes is normal.
   - Either fully approve or request changes. No middle ground.
   - You MUST actually run the gh pr review command, not just say "approved".

7. After submitting the review, verify it worked: gh pr view %s --json reviewDecision
   If the reviewDecision is still empty after your review, try again.

End your response with either APPROVED or CHANGES_REQUESTED on its own line.
`

const reReviewPromptTemplate = `You previously reviewed PR #%s for ticket %s and requested changes.
The engineer has pushed fixes. Check that your specific concerns were addressed.

## PR
%s

## Instructions
1. Read your previous review comments: gh pr view %s --comments
2. Read the latest diff: gh pr diff %s
3. For EACH concern you raised previously, verify it was addressed:
   - Was the fix correct?
   - Were tests added or updated?
   - Did the fix introduce new issues?

4. Post your findings as a PR comment with numbered items:
   gh pr comment %s --body 'your re-review findings'

5. Submit your review:
   If ALL concerns were addressed: gh pr review %s --approve --body "All feedback addressed"
   If concerns remain: gh pr review %s --request-changes --body "short summary of remaining issues"

6. Verify the review: gh pr view %s --json reviewDecision
   If the reviewDecision is still empty after your review, try again.

Focus on YOUR previous comments specifically — don't re-review the entire diff.
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
8. Push to the same branch — do NOT create a new PR
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
