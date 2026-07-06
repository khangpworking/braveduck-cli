package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/apply"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// validStages guards `status` against typos moving a job to a lane the board
// doesn't render.
var validStages = map[string]bool{
	"saved": true, "waiting": true, "applied": true,
	"interview": true, "offer": true, "closed": true,
}

func cmdAdd(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	title := fs.String("title", "", "job title (required)")
	company := fs.String("company", "", "company name")
	url := fs.String("url", "", "listing URL")
	salary := fs.String("salary", "", "salary text")
	source := fs.String("source", "", "discovery source (topcv/vietnamworks/...)")
	jd := fs.String("jd", "", "job description text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return errors.New("add: --title is required")
	}

	j := jobs.Job{
		Title:           *title,
		Company:         *company,
		URL:             *url,
		Salary:          *salary,
		FullJD:          *jd,
		DiscoverySource: *source,
		Status:          "saved",
		ScoreStatus:     "pending", // queued for a rater
	}
	// Wire the core apply classifier: read the contact email / email-vs-platform
	// method straight off the JD, so `add` isn't just a dumb insert.
	if *jd != "" {
		cls := apply.ClassifyApply(*jd)
		j.ApplyMethod = string(cls.Method)
		j.ContactEmail = cls.ContactEmail
	}
	// Fall back to the platform inferred from the URL as the discovery source.
	if j.DiscoverySource == "" && *url != "" {
		if p := apply.PlatformOf(*url); p != apply.PlatformNone {
			j.DiscoverySource = string(p)
		}
	}

	id, err := st.Add(ctx, j, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("added job #%d: %s\n", id, j.Title)
	return nil
}

func cmdList(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	status := fs.String("status", "", "filter by lane")
	source := fs.String("source", "", "filter by discovery source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	list, err := st.List(ctx, store.ListFilter{Status: *status, Source: *source})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no jobs")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tSCORE\tTITLE\tCOMPANY")
	for _, j := range list {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", j.ID, j.Status, scoreCell(j), trunc(j.Title, 40), trunc(j.Company, 24))
	}
	return w.Flush()
}

func cmdShow(ctx context.Context, st *store.Store, args []string) error {
	id, err := oneID(args, "show")
	if err != nil {
		return err
	}
	j, err := getJob(ctx, st, id)
	if err != nil {
		return err
	}
	p := func(label, v string) {
		if v != "" {
			fmt.Printf("  %-14s %s\n", label, v)
		}
	}
	fmt.Printf("#%d  %s\n", j.ID, j.Title)
	p("company", j.Company)
	p("status", j.Status)
	p("score", scoreCell(j))
	p("salary", j.Salary)
	p("source", j.DiscoverySource)
	p("url", j.URL)
	p("apply", strings.TrimSpace(j.ApplyMethod+" "+j.ApplyState))
	p("contact", j.ContactEmail)
	p("expires", j.ExpiresAt)
	p("notes", j.Notes)
	if j.Pinned {
		p("pinned", "yes")
	}
	if j.Keep {
		p("keep", "yes")
	}
	return nil
}

func cmdStatus(ctx context.Context, st *store.Store, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: braveduck status <id> <stage>")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	stage := strings.ToLower(args[1])
	if !validStages[stage] {
		return fmt.Errorf("invalid stage %q (saved/waiting/applied/interview/offer/closed)", stage)
	}
	if err := mapMissing(st.SetStatus(ctx, id, stage, time.Now()), id); err != nil {
		return err
	}
	fmt.Printf("job #%d → %s\n", id, stage)
	return nil
}

func cmdToggle(ctx context.Context, st *store.Store, args []string, kind string) error {
	id, err := oneID(args, kind)
	if err != nil {
		return err
	}
	j, err := getJob(ctx, st, id)
	if err != nil {
		return err
	}
	var now bool
	if kind == "pin" {
		j.Pinned = !j.Pinned
		now = j.Pinned
	} else {
		j.Keep = !j.Keep
		now = j.Keep
	}
	if err := st.Update(ctx, j, time.Now()); err != nil {
		return err
	}
	fmt.Printf("job #%d %s = %v\n", id, kind, now)
	return nil
}

func cmdDelete(ctx context.Context, st *store.Store, args []string) error {
	id, err := oneID(args, "rm")
	if err != nil {
		return err
	}
	if err := mapMissing(st.Delete(ctx, id), id); err != nil {
		return err
	}
	fmt.Printf("deleted job #%d\n", id)
	return nil
}

func cmdRateConfig(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("rate-config", flag.ContinueOnError)
	ab := fs.Bool("ab", false, "enable A/B challengers")
	models := fs.String("models", "", "comma-separated rater ids to set (1..3)")
	def := fs.String("default", "", "default rater id (owns the board badge)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// No --models → show mode.
	if strings.TrimSpace(*models) == "" {
		cfg, err := st.GetRatingConfig(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("ab_enabled: %v\nmodels:     %s\ndefault:    %s\nactive now: %s\n",
			cfg.ABEnabled, strings.Join(cfg.Models, ", "), cfg.DefaultModel,
			strings.Join(cfg.ActiveModels(), ", "))
		return nil
	}

	ids := splitCSV(*models)
	d := *def
	if d == "" {
		d = ids[0]
	}
	cfg, err := st.SetRatingConfig(ctx, rating.Config{ABEnabled: *ab, Models: ids, DefaultModel: d})
	if err != nil {
		return err
	}
	fmt.Printf("saved: ab=%v models=[%s] default=%s\n",
		cfg.ABEnabled, strings.Join(cfg.Models, ", "), cfg.DefaultModel)
	return nil
}

// --- shared helpers ---

func getJob(ctx context.Context, st *store.Store, id int64) (jobs.Job, error) {
	j, err := st.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, fmt.Errorf("no job #%d", id)
	}
	return j, err
}

// mapMissing turns the store's ErrNoRows into a friendly "no job #N".
func mapMissing(err error, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no job #%d", id)
	}
	return err
}

func oneID(args []string, cmd string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("usage: braveduck %s <id>", cmd)
	}
	return parseID(args[0])
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("bad id %q", s)
	}
	return id, nil
}

func scoreCell(j jobs.Job) string {
	if !j.HasScore {
		if j.ScoreStatus == "pending" {
			return "·"
		}
		return "-"
	}
	if j.ScoreLetter != "" {
		return fmt.Sprintf("%s %.1f", j.ScoreLetter, j.Score)
	}
	return fmt.Sprintf("%.1f", j.Score)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
