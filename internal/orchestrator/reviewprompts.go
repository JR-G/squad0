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

5. You MUST submit your review using one of these gh commands — this is MANDATORY:
   To approve: gh pr review %s --approve --body "your review summary"
   To request changes: gh pr review %s --request-changes --body "your detailed feedback"

   IMPORTANT RULES:
   - Do NOT say "approved with comments" — that is NOT an approval.
   - If you have ANY concerns, use --request-changes. Don't be afraid to request changes.
   - The pipeline handles the fix-up loop automatically — requesting changes is normal.
   - Either fully approve or request changes. No middle ground.
   - You MUST actually run the gh pr review command, not just say "approved".

6. After submitting the review, verify it worked: gh pr view %s --json reviewDecision

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

4. Submit your review:
   If ALL concerns were addressed: gh pr review %s --approve --body "All feedback addressed"
   If concerns remain: gh pr review %s --request-changes --body "specific remaining issues"

5. Verify the review: gh pr view %s --json reviewDecision

Focus on YOUR previous comments specifically — don't re-review the entire diff.
End with APPROVED or CHANGES_REQUESTED.
`

const fixUpPromptTemplate = `You need to address review feedback on your PR for ticket %s.

## PR
%s

## Instructions
1. Read the review comments: gh pr view %s --comments
2. Read the current diff: gh pr diff %s
3. Address every piece of feedback — fix the code, update tests, handle edge cases
4. If the branch is behind main, rebase: git fetch origin main && git rebase origin/main
5. Commit your fixes with conventional commit messages
6. Push to the same branch — do NOT create a new PR

Focus on what the reviewer asked for. Don't refactor unrelated code.
`

// BuildReviewPrompt creates the prompt for a reviewer session.
func BuildReviewPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(reviewPromptTemplate, prNum, ticket, prURL, prNum, prNum, prNum, prNum, prNum, prNum)
}

// BuildReReviewPrompt creates the prompt for re-reviewing after fixes.
func BuildReReviewPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(reReviewPromptTemplate, prNum, ticket, prURL, prNum, prNum, prNum, prNum, prNum)
}

// BuildFixUpPrompt creates the prompt for an engineer to address review feedback.
func BuildFixUpPrompt(prURL, ticket string) string {
	prNum := ExtractPRNumber(prURL)
	return fmt.Sprintf(fixUpPromptTemplate, ticket, prURL, prNum, prNum)
}
