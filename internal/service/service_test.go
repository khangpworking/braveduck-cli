package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st)
	s.now = func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }
	s.fetch = func(context.Context, string) (scrape.JobPosting, bool, error) {
		return scrape.JobPosting{Title: "Growth Marketer", Company: "Duck Co", Deadline: "2026-12-01"}, true, nil
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func TestAddJobAppliesRulesAndLogs(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	if _, err := s.AddJob(ctx, NewJob{Title: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank title = %v, want ErrInvalid", err)
	}

	j, err := s.AddJob(ctx, NewJob{
		Title: "Data Analyst",
		URL:   "https://www.topcv.vn/viec-lam/x",
		JD:    "Gửi CV về hr@duck.vn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != "saved" || j.ScoreStatus != "pending" || j.DiscoverySource != "topcv" {
		t.Fatalf("defaults not applied: status=%q score=%q source=%q", j.Status, j.ScoreStatus, j.DiscoverySource)
	}
	if j.ContactEmail != "hr@duck.vn" || j.ApplyMethod == "" {
		t.Fatalf("JD not classified: email=%q method=%q", j.ContactEmail, j.ApplyMethod)
	}

	// The CLI path now feeds the activity feed too.
	ev, _ := s.Activity(ctx, 5)
	if len(ev) != 1 || ev[0].Type != "added" {
		t.Fatalf("activity = %+v, want one 'added'", ev)
	}

	if _, err := s.AddJob(ctx, NewJob{Title: "dup", URL: "https://www.topcv.vn/viec-lam/x"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate URL = %v, want ErrDuplicate", err)
	}
}

func TestAddFromContent(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	j, err := s.AddFromContent(ctx, "https://www.vietnamworks.com/job/1")
	if err != nil {
		t.Fatal(err)
	}
	if j.Title != "Growth Marketer" || j.ExpiresAt != "expire:2026-12-01" || j.DiscoverySource != "vietnamworks" {
		t.Fatalf("extract not applied: %+v", j)
	}

	fetched := false
	s.fetch = func(context.Context, string) (scrape.JobPosting, bool, error) {
		fetched = true
		return scrape.JobPosting{}, false, nil
	}
	if _, err := s.AddFromContent(ctx, "https://www.vietnamworks.com/job/1"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("re-add = %v, want ErrDuplicate", err)
	}
	if fetched {
		t.Fatal("duplicate URL was fetched before being rejected")
	}

	s.fetch = scrape.FetchJobPosting // real guard, no network: loopback is refused before any fetch
	if _, err := s.AddFromContent(ctx, "http://127.0.0.1:6379/"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("internal URL = %v, want ErrInvalid", err)
	}

	if _, err := s.AddFromContent(ctx, "hi"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too-short paste = %v, want ErrInvalid", err)
	}
}

func TestUpdateJobValidatesAndLogsMoves(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "X"})

	if _, err := s.SetStatus(ctx, j.ID, "banana"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad stage = %v, want ErrInvalid", err)
	}
	got, err := s.SetStatus(ctx, j.ID, " Applied ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "applied" {
		t.Fatalf("status = %q, want normalized 'applied'", got.Status)
	}
	got, err = s.UpdateJob(ctx, j.ID, Patch{Notes: ptr("call back Monday")})
	if err != nil || got.Notes != "call back Monday" || got.Status != "applied" {
		t.Fatalf("notes patch: %v %+v", err, got)
	}

	ev, _ := s.Activity(ctx, 5)
	if len(ev) != 2 || ev[0].Type != "moved" {
		t.Fatalf("activity = %+v, want added then moved (and no event for a notes edit)", ev)
	}

	if _, err := s.SetStatus(ctx, 999, "applied"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing job = %v, want ErrNotFound", err)
	}
}

func TestTogglesAndDelete(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "X"})

	if got, _ := s.TogglePin(ctx, j.ID); !got.Pinned {
		t.Fatal("pin didn't toggle on")
	}
	if got, _ := s.ToggleKeep(ctx, j.ID); !got.Keep || !got.Pinned {
		t.Fatal("keep didn't toggle on independently")
	}
	if _, err := s.TogglePin(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("toggle missing = %v", err)
	}

	del, err := s.DeleteJob(ctx, j.ID)
	if err != nil || del.Title != "X" {
		t.Fatalf("delete: %v %+v", err, del)
	}
	if _, err := s.DeleteJob(ctx, j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete = %v, want ErrNotFound", err)
	}
}

func TestSetRatingConfigInvalidIsErrInvalid(t *testing.T) {
	s := newSvc(t)
	_, err := s.SetRatingConfig(context.Background(), rating.Config{Models: []string{"a"}, DefaultModel: "b"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestStats(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	for _, stage := range []string{"saved", "interview", "closed", "closed"} {
		j, _ := s.AddJob(ctx, NewJob{Title: stage})
		if _, err := s.SetStatus(ctx, j.ID, stage); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 4 || st.ByStage["closed"] != 2 || st.ByStage["offer"] != 0 {
		t.Fatalf("counts wrong: %+v", st)
	}
	if st.Resolved != 3 || st.Interviews != 1 || st.Morale == "" {
		t.Fatalf("rate wrong: %+v", st)
	}
}

func TestReportCrawl(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if err := s.ReportCrawl(ctx, "Face Book", true, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad source name = %v", err)
	}
	if err := s.ReportCrawl(ctx, "Facebook", true, ""); err != nil {
		t.Fatal(err)
	}
	v, _ := s.CrawlStatus(ctx)
	if len(v.Sources) != 1 || v.Sources[0].Name != "facebook" || !v.Sources[0].Healthy {
		t.Fatalf("crawl status = %+v", v)
	}

	// The feed gets one line when a source starts failing, not one per poll.
	s.ReportCrawl(ctx, "facebook", false, "logged out")
	s.ReportCrawl(ctx, "facebook", false, "logged out")
	ev, _ := s.Activity(ctx, 10)
	n := 0
	for _, e := range ev {
		if e.Type == "error" {
			n++
		}
	}
	if n != 1 || !strings.Contains(ev[0].Message, "facebook crawl failed: logged out") {
		t.Fatalf("feed = %+v, want exactly one failure line", ev)
	}
}
