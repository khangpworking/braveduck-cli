package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// cmdJev: `jev` status, `jev enable`, `jev rate [<id>|--all]`.
func cmdJev(ctx context.Context, svc *service.Service, args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "status":
		src := service.JevKeySource()
		if src == "" {
			src = "none — set TYPESAFE_API_KEY or create ~/.config/jev-route/typesafe.env"
		}
		cfg, err := svc.RatingConfig(ctx)
		if err != nil {
			return err
		}
		role := "not used (run: braveduck jev enable)"
		if service.IsJevModel(cfg.DefaultModel) {
			role = "default rater — scores new jobs automatically while the board runs"
		} else {
			for _, m := range cfg.Models {
				if service.IsJevModel(m) {
					role = "challenger"
				}
			}
		}
		fmt.Printf("key:  %s\nrole: %s\nsends: your full profile + each job's text to api.typesafe.ai\n", src, role)
		return nil
	case "enable":
		cfg, err := svc.EnableJev(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Jev is the default rater (%s). Challengers: %s\n", cfg.DefaultModel, strings.Join(cfg.Models[1:], ", "))
		fmt.Println("New jobs are rated automatically while the board runs; now: braveduck jev rate --all")
		return nil
	case "rate":
		if len(args) == 1 && args[0] == "--all" {
			n, err := svc.JevRateQueue(ctx, 50)
			fmt.Printf("rated %d job(s) with Jev\n", n)
			return err
		}
		id, err := oneID(args, "jev rate")
		if err != nil {
			return err
		}
		j, err := svc.JevRate(ctx, id)
		if err != nil {
			return friendly(err, id)
		}
		fmt.Printf("job #%d: %s %.1f — %s\n", j.ID, j.ScoreLetter, j.Score, j.ScoreReasoning)
		return nil
	}
	return errors.New("usage: braveduck jev [status | enable | rate <id> | rate --all]")
}
