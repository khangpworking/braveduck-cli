package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
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
		fmt.Println(ratedLine(j, *model))
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
	fmt.Print(service.RenderRatingBrief(brief))
	return nil
}

// ratedLine reports the verdict just saved: the board's badge for the
// default rater, or the challenger's own rating (which doesn't move the badge).
func ratedLine(j jobs.Job, model string) string {
	if r, ok := j.Ratings[model]; ok && model != "" {
		line := fmt.Sprintf("job #%d: %s rated %s %.1f", j.ID, model, r.Letter, r.Score)
		if j.ScoreLetter != "" && j.ScoreLetter != r.Letter {
			line += fmt.Sprintf(" (the board's badge stays %s %.1f)", j.ScoreLetter, j.Score)
		}
		return line
	}
	return fmt.Sprintf("job #%d rated %s %.1f", j.ID, j.ScoreLetter, j.Score)
}
