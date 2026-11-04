package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
)

type fakeCrawler struct {
	byQuery map[string][]crawl.Listing
	err     error
	calls   atomic.Int32
}

func (f *fakeCrawler) Name() string { return "vietnamworks" }

func (f *fakeCrawler) Search(_ context.Context, q string) ([]crawl.Listing, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.byQuery[q], nil
}

func crawlSvc(t *testing.T) (*Service, *fakeCrawler) {
	t.Helper()
	s := newSvc(t)
	engineer := crawl.Listing{Title: "n8n Automation Engineer", Company: "Duck Co", URL: "https://vnw/1", Deadline: "2026-12-04", Skills: []string{"n8n", "Python"}}
	fc := &fakeCrawler{byQuery: map[string][]crawl.Listing{
		"n8n": {
			engineer,
			{Title: "Senior Sales Manager", Company: "Duck Co", URL: "https://vnw/2"},
		},
		"ai automation": {
			engineer, // the same job matches both queries
			{Title: "AI Automation Lead", Company: "Spam Agency", URL: "https://vnw/3"},
		},
	}}
	s.SetCrawlers(fc)
	return s, fc
}

func addTerms(t *testing.T, s *Service, list string, terms ...string) {
	t.Helper()
	if _, err := s.st.AddKeywords(context.Background(), list, terms); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlFiltersDedupsAndReports(t *testing.T) {
	s, _ := crawlSvc(t)
	ctx := context.Background()

	if _, err := s.Crawl(ctx, "vietnamworks"); !errors.Is(err, ErrNoQueries) {
		t.Fatalf("no queries = %v", err)
	}

	addTerms(t, s, "search_queries", "n8n", "ai automation")
	addTerms(t, s, "negative_title", "sales")
	addTerms(t, s, "negative_company", "spam agency")
	res, err := s.Crawl(ctx, "VietnamWorks")
	if err != nil {
		t.Fatal(err)
	}
	if res.Queries != 2 || res.Found != 3 || res.Filtered != 2 || res.Added != 1 {
		t.Fatalf("result = %+v, want 2 queries, 3 unique found, 2 filtered, 1 added", res)
	}
	list, _ := s.ListJobs(ctx, ListFilter{Source: "vietnamworks"})
	if len(list) != 1 || list[0].ExpiresAt != "expire:2026-12-04" || list[0].Skills != "n8n, Python" || list[0].ScoreStatus != "pending" {
		t.Fatalf("stored = %+v", list)
	}

	// Second run: nothing new, no duplicate.
	if res, _ := s.Crawl(ctx, "vietnamworks"); res.Added != 0 {
		t.Fatalf("second run added %d", res.Added)
	}

	v, _ := s.CrawlStatus(ctx)
	if len(v.Sources) != 1 || !v.Sources[0].Healthy {
		t.Fatalf("crawl panel = %+v", v)
	}
	ev, _ := s.Activity(ctx, 5)
	summaries := 0
	for _, e := range ev {
		if strings.Contains(e.Message, "vietnamworks: 1 new job") {
			summaries++
		}
		if strings.HasPrefix(e.Message, "Added ") {
			t.Fatalf("per-job feed line from a crawl: %q", e.Message)
		}
	}
	if summaries != 1 {
		t.Fatalf("feed = %+v, want one summary line", ev)
	}
}

func TestCrawlFailureIsReported(t *testing.T) {
	s, fc := crawlSvc(t)
	ctx := context.Background()
	addTerms(t, s, "search_queries", "n8n")
	fc.err = errors.New("HTTP 403: forbidden")
	if _, err := s.Crawl(ctx, "vietnamworks"); err == nil {
		t.Fatal("failure not returned")
	}
	v, _ := s.CrawlStatus(ctx)
	if v.Sources[0].Healthy || !strings.Contains(v.Sources[0].LastError, "403") {
		t.Fatalf("crawl panel = %+v", v.Sources[0])
	}
	if _, err := s.Crawl(ctx, "linkedin"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown crawler = %v", err)
	}
}

func TestCrawlSchedules(t *testing.T) {
	s, _ := crawlSvc(t)
	ctx := context.Background()
	s.ReportCrawl(ctx, "facebook", true, "") // an external source

	got, _ := s.CrawlSchedules(ctx)
	if len(got) != 2 || got[1].Source != "vietnamworks" || got[1].EveryMinutes != DefaultEveryMinutes || got[1].Configured || !got[1].Builtin ||
		got[0].Source != "facebook" || got[0].Builtin || got[0].EveryMinutes != 0 {
		t.Fatalf("schedules = %+v", got)
	}

	for _, bad := range []struct {
		src string
		min int
	}{{"vietnamworks", 10}, {"facebook", 3}, {"facebook", 2000}, {"Face Book", 30}} {
		if _, err := s.SetCrawlSchedule(ctx, bad.src, bad.min); !errors.Is(err, ErrInvalid) {
			t.Errorf("SetCrawlSchedule(%q, %d) = %v, want ErrInvalid", bad.src, bad.min, err)
		}
	}
	if _, err := s.SetCrawlSchedule(ctx, "facebook", 5); err != nil {
		t.Fatalf("external source at 5 min: %v", err)
	}
	if _, err := s.SetCrawlSchedule(ctx, "vietnamworks", 0); err != nil {
		t.Fatal(err)
	}
	got, _ = s.CrawlSchedules(ctx)
	if got[0].EveryMinutes != 5 || got[1].EveryMinutes != 0 || !got[1].Configured {
		t.Fatalf("after set = %+v", got)
	}
}

func TestSchedulerRunsWhenDue(t *testing.T) {
	s, fc := crawlSvc(t)
	ctx := context.Background()
	addTerms(t, s, "search_queries", "n8n")
	clock := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }

	s.runDue(ctx) // never run → due
	if fc.calls.Load() != 1 {
		t.Fatalf("first tick: %d calls", fc.calls.Load())
	}
	clock = clock.Add(time.Hour)
	s.runDue(ctx) // default 2h: not yet
	if fc.calls.Load() != 1 {
		t.Fatalf("ran before its interval: %d calls", fc.calls.Load())
	}
	clock = clock.Add(61 * time.Minute)
	s.runDue(ctx)
	if fc.calls.Load() != 2 {
		t.Fatalf("didn't run when due: %d calls", fc.calls.Load())
	}
	s.SetCrawlSchedule(ctx, "vietnamworks", 0)
	clock = clock.Add(5 * time.Hour)
	s.runDue(ctx)
	if fc.calls.Load() != 2 {
		t.Fatal("ran while switched off")
	}
}
