package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

func cmdAdd(ctx context.Context, svc *service.Service, args []string) error {
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
	if fs.NArg() > 0 {
		if *title != "" {
			return errors.New("add: pass a link/text or --title, not both")
		}
		return addFromContent(ctx, svc, fs.Args())
	}
	if strings.TrimSpace(*title) == "" {
		return errors.New("add: pass a job link or text, or --title (see: braveduck help)")
	}
	j, err := svc.AddJob(ctx, service.NewJob{
		Title: *title, Company: *company, URL: *url,
		Salary: *salary, Source: *source, JD: *jd,
	})
	if err != nil {
		return err
	}
	fmt.Printf("added job #%d: %s\n", j.ID, j.Title)
	return nil
}

// addFromContent is `braveduck add <link> [text…]`: the board's paste box.
// A "-" argument reads that part from stdin (for a long post).
func addFromContent(ctx context.Context, svc *service.Service, args []string) error {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			a = string(b)
		}
		parts[i] = a
	}
	j, err := svc.AddFromContent(ctx, strings.Join(parts, "\n"))
	if err != nil {
		return fmt.Errorf("add: not added — %w", err)
	}
	fmt.Printf("added job #%d: %s\n", j.ID, j.Title)
	if j.Company != "" {
		fmt.Printf("  company  %s\n", j.Company)
	}
	if j.Salary != "" {
		fmt.Printf("  salary   %s\n", j.Salary)
	}
	return nil
}

// cmdToday prints what to act on now (the board's Today panel).
func cmdToday(ctx context.Context, svc *service.Service) error {
	groups, err := svc.Today(ctx)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		fmt.Println("nothing needs you today")
		return nil
	}
	for i, g := range groups {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(g.Title)
		for _, it := range g.Items {
			fmt.Printf("  #%-4d %s  (%s)\n", it.JobID, trunc(it.Title, 60), it.Why)
		}
	}
	return nil
}

func cmdList(ctx context.Context, svc *service.Service, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	status := fs.String("status", "", "filter by lane")
	source := fs.String("source", "", "filter by discovery source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	list, err := svc.ListJobs(ctx, service.ListFilter{Status: *status, Source: *source})
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

func cmdShow(ctx context.Context, svc *service.Service, args []string) error {
	id, err := oneID(args, "show")
	if err != nil {
		return err
	}
	j, err := svc.GetJob(ctx, id)
	if err != nil {
		return friendly(err, id)
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

func cmdStatus(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: braveduck status <id> <stage>")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	j, err := svc.SetStatus(ctx, id, args[1])
	if err != nil {
		return friendly(err, id)
	}
	fmt.Printf("job #%d → %s\n", id, j.Status)
	return nil
}

func cmdToggle(ctx context.Context, svc *service.Service, args []string, kind string) error {
	id, err := oneID(args, kind)
	if err != nil {
		return err
	}
	var j jobs.Job
	var now bool
	if kind == "pin" {
		j, err = svc.TogglePin(ctx, id)
		now = j.Pinned
	} else {
		j, err = svc.ToggleKeep(ctx, id)
		now = j.Keep
	}
	if err != nil {
		return friendly(err, id)
	}
	fmt.Printf("job #%d %s = %v\n", id, kind, now)
	return nil
}

func cmdDelete(ctx context.Context, svc *service.Service, args []string) error {
	id, err := oneID(args, "rm")
	if err != nil {
		return err
	}
	if _, err := svc.DeleteJob(ctx, id); err != nil {
		return friendly(err, id)
	}
	fmt.Printf("deleted job #%d\n", id)
	return nil
}

func cmdRateConfig(ctx context.Context, svc *service.Service, args []string) error {
	fs := flag.NewFlagSet("rate-config", flag.ContinueOnError)
	ab := fs.Bool("ab", false, "enable A/B challengers")
	models := fs.String("models", "", "comma-separated rater ids to set (1..3)")
	def := fs.String("default", "", "default rater id (owns the board badge)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// No --models → show mode.
	if strings.TrimSpace(*models) == "" {
		cfg, err := svc.RatingConfig(ctx)
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
	cfg, err := svc.SetRatingConfig(ctx, rating.Config{ABEnabled: *ab, Models: ids, DefaultModel: d})
	if err != nil {
		return err
	}
	fmt.Printf("saved: ab=%v models=[%s] default=%s\n",
		cfg.ABEnabled, strings.Join(cfg.Models, ", "), cfg.DefaultModel)
	return nil
}

// --- shared helpers ---

// friendly turns the service's not-found into "no job #N" for the terminal.
func friendly(err error, id int64) error {
	if errors.Is(err, service.ErrNotFound) {
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

// trunc shortens s to at most n characters (runes, not bytes: cutting bytes
// splits Vietnamese letters into "�").
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
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

// cmdEdit changes a job's details; only the flags given change.
func cmdEdit(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: braveduck edit <id> [--title T] [--company C] [--salary S] [--location L] [--notes N]")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	var p service.Patch
	for name, dst := range map[string]**string{"title": &p.Title, "company": &p.Company, "salary": &p.Salary, "location": &p.Location, "notes": &p.Notes} {
		dst := dst
		fs.Func(name, "new "+name, func(v string) error { *dst = &v; return nil })
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if p == (service.Patch{}) {
		return errors.New("nothing to change — pass e.g. --company \"New name\"")
	}
	j, err := svc.UpdateJob(ctx, id, p)
	if err != nil {
		return friendly(err, id)
	}
	fmt.Printf("job #%d updated: %s — %s\n", j.ID, j.Title, j.Company)
	return nil
}
