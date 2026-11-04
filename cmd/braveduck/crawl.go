package main

import (
	"context"
	"errors"
	"flag"
	"strings"

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
