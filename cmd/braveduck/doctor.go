package main

import (
	"context"
	"fmt"
	"runtime"

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
	svc.SetProfilePath(profilePathFor(resolvedPath))
	if p, err := svc.Profile(ctx); err == nil {
		state := "filled in"
		if !p.Filled {
			state = "EMPTY — fill it in before rating"
		}
		fmt.Printf("  profile:    %s (%s)\n", p.Path, state)
	}
	fmt.Println("  status:     healthy")
	return nil
}
