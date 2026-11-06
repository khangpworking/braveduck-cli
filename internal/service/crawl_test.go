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
	if res, _ := s.Crawl(ctx, "vietnamworks"); res.Added != 0 || res.Duplicates != 1 {
		t.Fatalf("second run added %d, skipped %d as already on the board, want 0 and 1", res.Added, res.Duplicates)
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

	got := schedByName(t, s)
	if v := got["vietnamworks"]; v.EveryMinutes != DefaultEveryMinutes || v.Configured || !v.Builtin {
		t.Fatalf("vietnamworks = %+v", v)
	}
	if f := got["facebook"]; f.Builtin || f.EveryMinutes != 0 {
		t.Fatalf("facebook = %+v", f)
	}
	if ts := got[TopCVSyncSource]; ts.EveryMinutes != 360 || !ts.Builtin {
		t.Fatalf("topcv-sync = %+v", ts)
	}
	if jr := got[JevRateSource]; jr.EveryMinutes != 30 || !jr.Builtin {
		t.Fatalf("jev-rate = %+v", jr)
	}

	// What the board's Job sources window goes by: names for people, which
	// rows are job sites, and the shortest interval each may be set to.
	if v := got["vietnamworks"]; v.Name != "VietnamWorks" || v.Task || v.MinEveryMinutes != MinBuiltinEveryMinutes {
		t.Fatalf("vietnamworks = %+v", v)
	}
	if f := got["facebook"]; f.Name != "Facebook" || f.Task || f.MinEveryMinutes != MinEveryMinutes {
		t.Fatalf("facebook = %+v", f)
	}
	for _, id := range []string{TopCVSyncSource, JevRateSource} {
		if task := got[id]; !task.Task || task.Name == "" || task.Name == id {
			t.Fatalf("%s = %+v", id, task)
		}
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
	got = schedByName(t, s)
	if got["facebook"].EveryMinutes != 5 || got["vietnamworks"].EveryMinutes != 0 || !got["vietnamworks"].Configured {
		t.Fatalf("after set = %+v", got)
	}
}

func TestSchedulerRunsWhenDue(t *testing.T) {
	s, fc := crawlSvc(t)
	ctx := context.Background()
	clock := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }

	// No search terms yet: nothing to crawl, and nothing reported as failing.
	s.runDue(ctx)
	if v, err := s.st.CrawlStatus(ctx, clock, nil); err != nil || len(v.Sources) != 0 {
		t.Fatalf("crawls reported before there were search terms: %+v %v", v.Sources, err)
	}

	addTerms(t, s, "search_queries", "n8n")
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

func schedByName(t *testing.T, s *Service) map[string]SourceSchedule {
	t.Helper()
	list, err := s.CrawlSchedules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]SourceSchedule{}
	for _, sc := range list {
		m[sc.Source] = sc
	}
	return m
}

// A job the user pasted (by link or as text) isn't added again when a crawl
// finds it. Two links with the same title are two listings (a re-post, or
// another city), so both are added.
func TestCrawlSkipsJobsAlreadyOnBoard(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.AddFromContent(ctx, "https://www.vietnamworks.com/growth-marketer-1-jv?utm_source=fb")
	s.AddFromContent(ctx, "Data Analyst\nMoMo\nSalary: $2,000")
	s.SetCrawlers(&fakeCrawler{byQuery: map[string][]crawl.Listing{"data": {
		{Title: "Growth Marketer (Hybrid)", Company: "Duck Co", URL: "https://www.vietnamworks.com/growth-marketer-1-jv"}, // same link
		{Title: "Data Analyst", Company: "MoMo", URL: "https://www.vietnamworks.com/data-analyst-2-jv"},                   // pasted as text
		{Title: "Product Owner", Company: "Duck Co", URL: "https://www.vietnamworks.com/product-owner-3-jv"},
		{Title: "Product Owner", Company: "Duck Co", URL: "https://www.vietnamworks.com/product-owner-3-jv-hcm"},
	}}})
	addTerms(t, s, "search_queries", "data")

	res, err := s.Crawl(ctx, "vietnamworks")
	if err != nil {
		t.Fatal(err)
	}
	if res.Found != 4 || res.Duplicates != 2 || res.Added != 2 {
		t.Fatalf("result = %+v, want 4 found, 2 already on the board, 2 added", res)
	}
	if list, _ := s.ListJobs(ctx, ListFilter{}); len(list) != 4 {
		t.Fatalf("board has %d jobs, want the 2 pasted + both Product Owner listings", len(list))
	}
}

// skipCrawler records the known-check the service hands a KnownSkipper.
type skipCrawler struct {
	fakeCrawler
	known func(string) bool
}

func (s *skipCrawler) SkipKnown(known func(string) bool) { s.known = known }

func TestCrawlTellsSkippersWhatsKnown(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.AddJob(ctx, NewJob{Title: "Pasted", URL: "https://www.careerlink.vn/tim-viec-lam/ai/1?utm_source=x"})
	sc := &skipCrawler{fakeCrawler: fakeCrawler{byQuery: map[string][]crawl.Listing{}}}
	s.SetCrawlers(sc)
	addTerms(t, s, "search_queries", "ai")
	if _, err := s.Crawl(ctx, "vietnamworks"); err != nil {
		t.Fatal(err)
	}
	if sc.known == nil || !sc.known("https://www.careerlink.vn/tim-viec-lam/ai/1") || sc.known("https://www.careerlink.vn/tim-viec-lam/other/2") {
		t.Fatal("the crawler wasn't given a check that knows the board's links")
	}
}

func TestCrawlStatusMarksTasks(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	for _, src := range []string{"careerlink", TopCVSyncSource, JevRateSource} {
		s.st.RecordCrawlRun(ctx, src, true, "", s.now())
	}
	v, err := s.CrawlStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range v.Sources {
		if want := src.Name != "careerlink"; src.Task != want {
			t.Errorf("%s: Task = %v, want %v", src.Name, src.Task, want)
		}
		if !src.Healthy {
			t.Errorf("%s unhealthy right after a successful run", src.Name)
		}
	}
}
