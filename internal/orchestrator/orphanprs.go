package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

var ticketFromBranch = regexp.MustCompile(`(?i)feat/(jam-\d+)`)

// recoverOrphanedPRs scans GitHub for open PRs that have no pipeline
// item. Creates pipeline items so the reviewer picks them up.
func (orch *Orchestrator) recoverOrphanedPRs(ctx context.Context) {
	if orch.pipelineStore == nil || orch.cfg.TargetRepoDir == "" {
		return
	}

	prs, err := listOpenPRs(ctx, orch.cfg.TargetRepoDir)
	if err != nil {
		log.Printf("orphan PR scan failed: %v", err)
		return
	}

	for _, pr := range prs {
		if orch.hasPipelineItem(ctx, pr.ticket) {
			continue
		}

		log.Printf("orphan PR: creating pipeline item for %s (%s)", pr.ticket, pr.url)
		itemID, createErr := orch.pipelineStore.Create(ctx, pipeline.WorkItem{
			Ticket:   pr.ticket,
			Engineer: guessEngineer(pr.branch),
			Stage:    pipeline.StagePROpened,
			Branch:   pr.branch,
		})
		if createErr != nil {
			log.Printf("failed to create pipeline item for orphan %s: %v", pr.ticket, createErr)
			continue
		}

		_ = orch.pipelineStore.SetPRURL(ctx, itemID, pr.url)
	}
}

func (orch *Orchestrator) hasPipelineItem(ctx context.Context, ticket string) bool {
	items, err := orch.pipelineStore.GetByTicket(ctx, ticket)
	if err != nil {
		return false
	}

	for _, item := range items {
		if !item.Stage.IsTerminal() {
			return true
		}
	}

	return false
}

type openPR struct {
	ticket string
	url    string
	branch string
}

func listOpenPRs(ctx context.Context, repoDir string) ([]openPR, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open",
		"--json", "number,headRefName,url",
		"--limit", "50")
	cmd.Dir = repoDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %s: %w", string(output), err)
	}

	return parseOpenPRs(string(output)), nil
}

// ParseOpenPRsForTest exports parseOpenPRs for testing.
func ParseOpenPRsForTest(output string) []openPR { return parseOpenPRs(output) }

// ExtractJSONFieldForTest exports extractJSONField for testing.
func ExtractJSONFieldForTest(line, field string) string { return extractJSONField(line, field) }

func parseOpenPRs(output string) []openPR {
	// Split JSON array into individual objects.
	entries := strings.Split(output, "},{")
	prs := make([]openPR, 0, len(entries))
	for _, line := range entries {
		line = strings.TrimSpace(line)

		branch := extractJSONField(line, "headRefName")
		url := extractJSONField(line, "url")

		if branch == "" || url == "" {
			continue
		}

		match := ticketFromBranch.FindStringSubmatch(branch)
		if len(match) < 2 {
			continue
		}

		prs = append(prs, openPR{
			ticket: strings.ToUpper(match[1]),
			url:    url,
			branch: branch,
		})
	}

	return prs
}

func extractJSONField(line, field string) string {
	key := `"` + field + `":"`
	idx := strings.Index(line, key)
	if idx == -1 {
		return ""
	}

	start := idx + len(key)
	end := strings.Index(line[start:], `"`)
	if end == -1 {
		return ""
	}

	return line[start : start+end]
}

func guessEngineer(branch string) agent.Role {
	// Can't determine which engineer opened the PR from the branch name.
	// Default to engineer-1 — the reviewer will assign correctly.
	return agent.RoleEngineer1
}
