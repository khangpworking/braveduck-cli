package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

func cmdProfile(ctx context.Context, svc *service.Service) error {
	p, err := svc.Profile(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("profile: %s\n", p.Path)
	if p.Filled {
		fmt.Println("status:  filled in — jobs can be rated against it")
	} else {
		fmt.Println("status:  empty — open the file, fill in what applies to you (any language), then rate")
	}
	return nil
}

// cmdRate prints a rating brief (for an agent or any AI chat) or, with
// --score, saves a verdict.
func cmdRate(ctx context.Context, svc *service.Service, args []string) error {
	// Allow `rate <id> --flags` as well as `rate --flags <id>`.
	var idArg string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		idArg, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("rate", flag.ContinueOnError)
	score := fs.Float64("score", 0, "save a verdict: score 1.0-5.0")
	why := fs.String("why", "", "reasoning (3-4 points separated by ' • ')")
	letter := fs.String("letter", "", "A-F (derived from --score when omitted)")
	model := fs.String("model", "", "rater id, only for an A/B challenger")
	limit := fs.Int("limit", 10, "how many unrated jobs to include")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if idArg == "" && fs.NArg() > 0 {
		idArg = fs.Arg(0)
	}

	if *score != 0 {
		if idArg == "" {
			return errors.New("usage: braveduck rate <id> --score 4.2 --why \"...\"")
		}
		id, err := parseID(idArg)
		if err != nil {
			return err
		}
		j, err := svc.SubmitRating(ctx, service.Verdict{
			JobID: id, Model: *model, Score: *score, Letter: *letter, Reasoning: *why,
		})
		if err != nil {
			return friendly(err, id)
		}
		fmt.Printf("job #%d rated %s %.1f\n", j.ID, j.ScoreLetter, j.Score)
		return nil
	}

	var brief service.RatingBrief
	var err error
	if idArg != "" {
		id, perr := parseID(idArg)
		if perr != nil {
			return perr
		}
		brief, err = svc.RateBrief(ctx, id)
		err = friendly(err, id)
	} else {
		brief, err = svc.RatingQueue(ctx, *limit)
	}
	if err != nil {
		return err
	}
	if len(brief.Jobs) == 0 {
		fmt.Println("nothing to rate — every open job has a score")
		return nil
	}
	fmt.Print(renderBrief(brief))
	return nil
}

// renderBrief turns a brief into a prompt for any AI chat.
func renderBrief(b service.RatingBrief) string {
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
	sb.WriteString("braveduck rate <id> --score <1.0-5.0> --why \"<point> • <point> • <point>\"\n")
	return sb.String()
}
