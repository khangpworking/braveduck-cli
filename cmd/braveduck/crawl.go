package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// cmdCrawlReport lets an external scraper (fb-watcher, cron jobs, …) record
// each run, so the board's crawl panel shows the source and flags it when it
// fails or stops reporting. Quiet on success: it's called every few minutes.
func cmdCrawlReport(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New(`usage: braveduck crawl-report <source> --ok | --fail "reason"`)
	}
	source, rest := args[0], args[1:]
	fs := flag.NewFlagSet("crawl-report", flag.ContinueOnError)
	ok := fs.Bool("ok", false, "the run succeeded")
	fail := fs.String("fail", "", "the run failed, and why")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *ok == (*fail != "") {
		return errors.New(`pass exactly one of --ok or --fail "reason"`)
	}
	return svc.ReportCrawl(ctx, source, *ok, *fail)
}

// cmdCrawl runs built-in crawlers now: `crawl` (all) or `crawl <source>`.
func cmdCrawl(ctx context.Context, svc *service.Service, args []string) error {
	sources := args
	if len(sources) == 0 {
		sources = svc.BuiltinCrawlers()
	}
	var failed error
	for _, src := range sources {
		res, err := svc.Crawl(ctx, src)
		if err != nil {
			fmt.Printf("%s: failed — %v\n", src, err)
			failed = err
			continue
		}
		fmt.Printf("%s: %d query(s), %d found, %d filtered out, %d new on the board\n",
			res.Source, res.Queries, res.Found, res.Filtered, res.Added)
	}
	return failed
}

// cmdCrawlSchedule shows or sets how often each source fetches new jobs.
//
//	crawl-schedule                    list every source
//	crawl-schedule <source>           print its interval in minutes (0 = off) —
//	                                  external tools read their setting this way
//	crawl-schedule <source> <2h|30m|1d|off>
func cmdCrawlSchedule(ctx context.Context, svc *service.Service, args []string) error {
	scheds, err := svc.CrawlSchedules(ctx)
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tEVERY\tRUN BY")
		for _, sc := range scheds {
			by := "its own tool (reads this setting)"
			if sc.Builtin {
				by = "braveduck, while the board runs"
			}
			every := formatEvery(sc.EveryMinutes)
			if !sc.Builtin && !sc.Configured {
				every = "not set"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", sc.Source, every, by)
		}
		return w.Flush()
	case 1:
		for _, sc := range scheds {
			if sc.Source == strings.ToLower(args[0]) {
				fmt.Println(sc.EveryMinutes)
				return nil
			}
		}
		fmt.Println(0)
		return nil
	case 2:
		min, err := parseEvery(args[1])
		if err != nil {
			return err
		}
		sc, err := svc.SetCrawlSchedule(ctx, args[0], min)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", sc.Source, formatEvery(sc.EveryMinutes))
		return nil
	}
	return errors.New("usage: braveduck crawl-schedule [<source> [<30m|2h|1d|off>]]")
}

// parseEvery reads "off", "45m", "2h" or "1d" as minutes.
func parseEvery(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "off" || s == "0" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil {
			return n * 24 * 60, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("can't read %q — use e.g. 30m, 2h, 1d or off", s)
	}
	return int(d / time.Minute), nil
}

func formatEvery(min int) string {
	switch {
	case min == 0:
		return "off"
	case min%(24*60) == 0:
		return fmt.Sprintf("every %dd", min/(24*60))
	case min%60 == 0:
		return fmt.Sprintf("every %dh", min/60)
	default:
		return fmt.Sprintf("every %dm", min)
	}
}
