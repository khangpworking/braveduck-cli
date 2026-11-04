package main

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// cmdDoctor is a quick environment + database health check: where the DB lives,
// whether it opens and migrates cleanly, and a couple of sanity counts. It's the
// one diagnostic command that doesn't assume the store already opened.
func cmdDoctor(dbPath string) error {
	fmt.Printf("braveduck doctor\n")
	fmt.Printf("  go runtime: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	resolved := dbPath
	if resolved == "" {
		if p, err := defaultDBPath(); err == nil {
			resolved = p + " (default)"
		}
	}
	fmt.Printf("  database:   %s\n", resolved)

	st, resolvedPath, err := openStore(dbPath)
	if err != nil {
		fmt.Printf("  db open:    FAIL — %v\n", err)
		return err
	}
	defer st.Close()
	ctx := context.Background()
	if v, err := st.SchemaVersion(ctx); err == nil {
		fmt.Printf("  db open:    ok (schema v%d)\n", v)
	}

	all, err := st.List(ctx, store.ListFilter{})
	if err != nil {
		fmt.Printf("  job count:  FAIL — %v\n", err)
		return err
	}
	fmt.Printf("  jobs:       %d\n", len(all))

	if cfg, err := st.GetRatingConfig(ctx); err == nil {
		fmt.Printf("  rater:      default=%s ab=%v\n", cfg.DefaultModel, cfg.ABEnabled)
	}
	svc := service.New(st)
	svc.SetDataDir(filepath.Dir(resolvedPath))
	if p, err := svc.Profile(ctx); err == nil {
		state := "filled in"
		if !p.Filled {
			state = "EMPTY — fill it in before rating"
		}
		fmt.Printf("  profile:    %s (%s)\n", p.Path, state)
	}
	if su, err := svc.Setup(ctx); err == nil {
		for _, line := range setupLines(su) {
			fmt.Println(line)
		}
	}
	if src := service.JevKeySource(); src != "" {
		fmt.Printf("  jev:        key found (%s)\n", src)
	}
	if src, err := svc.KeySource(); err == nil {
		fmt.Printf("  sessions:   encrypted with %s\n", src)
	} else {
		fmt.Printf("  sessions:   FAIL — %v\n", err)
	}
	if ps, err := svc.Platforms(ctx); err == nil {
		for _, p := range ps {
			if p.Status != "not_connected" {
				fmt.Printf("              %s: %s\n", p.ID, p.Status)
			}
		}
	}
	if hint := browser.LibsHint(); hint != "" {
		fmt.Printf("  browser:    missing system libraries%s\n", hint)
	}
	fmt.Println("  status:     healthy")
	return nil
}

// setupLines is doctor's view of the board's "Get started" steps: what's
// done, and the command for what isn't.
func setupLines(su service.SetupStatus) []string {
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	lines := []string{
		fmt.Sprintf("  setup:      %s CV / about me   %s", mark(su.CV || su.AboutMe), ifNot(su.CV || su.AboutMe, "braveduck cv upload <file.pdf|.docx|.md|.html>")),
		fmt.Sprintf("              %s what you want   %s", mark(su.Wants), ifNot(su.Wants, "edit the profile (👤 Profile on the board) — roles, pay, location, deal-breakers")),
		fmt.Sprintf("              %s search terms    %s", mark(su.SearchQueries > 0), ifNot(su.SearchQueries > 0, `braveduck keywords add search_queries "your role"`)),
	}
	if su.SearchQueries > 0 {
		lines[2] = fmt.Sprintf("              ✓ search terms    %d", su.SearchQueries)
	}
	lines = append(lines,
		fmt.Sprintf("              %s location        %s", mark(su.Location), ifNot(su.Location, "optional: 📍 Location on the board, for job distances")),
		fmt.Sprintf("              %s TopCV login     %s", mark(su.TopCV), ifNot(su.TopCV, "optional: braveduck login topcv, for status sync and apply")))
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

func ifNot(ok bool, hint string) string {
	if ok {
		return ""
	}
	return hint
}
