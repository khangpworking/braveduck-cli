package service

import (
	"fmt"
	"strings"
)

// RenderRatingBrief turns a rating brief into a prompt for any AI chat.
func RenderRatingBrief(b RatingBrief) string {
	var sb strings.Builder
	sb.WriteString("You are rating job listings for one candidate.\n\n")
	sb.WriteString("## How to score\n\n" + b.Rubric + "\n\n")
	sb.WriteString("## The candidate\n\n" + strings.TrimSpace(b.Profile) + "\n\n")
	sb.WriteString("## Jobs\n")
	for _, j := range b.Jobs {
		fmt.Fprintf(&sb, "\n### Job %d: %s", j.ID, j.Title)
		if j.Company != "" {
			fmt.Fprintf(&sb, " — %s", j.Company)
		}
		sb.WriteString("\n")
		for _, kv := range [][2]string{{"Location", j.Location}, {"Salary", j.Salary}, {"Skills", j.Skills}, {"URL", j.URL}} {
			if kv[1] != "" {
				fmt.Fprintf(&sb, "- %s: %s\n", kv[0], kv[1])
			}
		}
		if j.Description != "" {
			sb.WriteString("\n" + strings.TrimSpace(j.Description) + "\n")
		}
	}
	sb.WriteString("\n## Answer\n\nFor each job, reply with one line:\n")
	sb.WriteString("braveduck rate <id>")
	if b.Model != "" {
		// Model ids are free text. Keep a copied command literal even when
		// the id contains quotes or shell substitutions.
		fmt.Fprintf(&sb, " --model '%s'", strings.ReplaceAll(b.Model, "'", "'\"'\"'"))
	}
	sb.WriteString(" --score <1.0-5.0> --why \"<point> • <point> • <point>\"\n")
	return sb.String()
}

// RenderDocBrief turns a CV or email brief into a prompt for any AI chat,
// ending with how to hand the result back to braveduck.
func RenderDocBrief(b DocBrief) string {
	var sb strings.Builder
	what := map[string]string{"cv": "a tailored CV", "draft": "an application email"}[b.Kind]
	fmt.Fprintf(&sb, "Write %s for one candidate and one job.\n\n## Instructions\n\n%s\n\n", what, b.Instructions)
	sb.WriteString("## The candidate\n\n" + strings.TrimSpace(b.Profile) + "\n\n")
	j := b.Job
	fmt.Fprintf(&sb, "## The job (#%d): %s", j.ID, j.Title)
	if j.Company != "" {
		fmt.Fprintf(&sb, " — %s", j.Company)
	}
	sb.WriteString("\n")
	for _, kv := range [][2]string{{"Location", j.Location}, {"Salary", j.Salary}, {"Skills", j.Skills}, {"URL", j.URL}, {"Contact", b.ContactEmail}} {
		if kv[1] != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", kv[0], kv[1])
		}
	}
	if j.Description != "" {
		sb.WriteString("\n" + strings.TrimSpace(j.Description) + "\n")
	}
	if b.Notes != "" {
		sb.WriteString("\n## The candidate's notes for this draft\n\n" + b.Notes + "\n")
	}
	if b.Previous != "" {
		sb.WriteString("\n## Current version (improve it)\n\n" + b.Previous + "\n")
	}
	if b.Kind == "cv" {
		fmt.Fprintf(&sb, "\n## Answer\n\nReply with the CV in Markdown only. Save it as cv.md and run:\nbraveduck cv %d --file cv.md\n", j.ID)
	} else {
		fmt.Fprintf(&sb, "\n## Answer\n\nFirst line: \"Subject: …\", then a blank line, then the body. Save it as email.txt and run:\nbraveduck draft %d --file email.txt\n", j.ID)
	}
	return sb.String()
}

// AgentPrompt is the short request to paste into an agent that has braveduck
// connected over MCP (e.g. Claude Code).
func AgentPrompt(kind string, id int64, title string) string {
	switch kind {
	case "rate":
		return fmt.Sprintf("Using braveduck, rate job #%d (%s) against my profile.", id, title)
	case "cv":
		return fmt.Sprintf("Using braveduck, tailor my CV for job #%d (%s) and save it.", id, title)
	default:
		return fmt.Sprintf("Using braveduck, draft my application email for job #%d (%s) and save it.", id, title)
	}
}
