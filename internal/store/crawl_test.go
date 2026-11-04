package store

import (
	"context"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func source(t *testing.T, s *Store, at time.Time, name string) CrawlSource {
	t.Helper()
	v, err := s.CrawlStatus(context.Background(), at, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range v.Sources {
		if src.Name == name {
			return src
		}
	}
	t.Fatalf("source %q not listed: %+v", name, v.Sources)
	return CrawlSource{}
}

func TestCrawlRunHealth(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	t0 := now

	if err := s.RecordCrawlRun(ctx, "facebook", true, "", t0); err != nil {
		t.Fatal(err)
	}
	s.Add(ctx, jobs.Job{Title: "lead", DiscoverySource: "facebook"}, t0)
	if src := source(t, s, t0.Add(10*time.Minute), "facebook"); !src.Healthy || src.Count != 1 || src.LastError != "" {
		t.Fatalf("fresh success = %+v", src)
	}

	// A crashed watcher stops reporting: stalled after StaleAfter, same day.
	if src := source(t, s, t0.Add(StaleAfter+time.Minute), "facebook"); src.Healthy {
		t.Fatalf("silent for over an hour but healthy: %+v", src)
	}

	// A failure is unhealthy at once and carries its reason; success clears it.
	s.RecordCrawlRun(ctx, "facebook", false, "logged out", t0.Add(20*time.Minute))
	if src := source(t, s, t0.Add(21*time.Minute), "facebook"); src.Healthy || src.LastError != "logged out" {
		t.Fatalf("after failure = %+v", src)
	}
	s.RecordCrawlRun(ctx, "facebook", true, "ignored on success", t0.Add(30*time.Minute))
	if src := source(t, s, t0.Add(31*time.Minute), "facebook"); !src.Healthy || src.LastError != "" {
		t.Fatalf("after recovery = %+v", src)
	}
}

func TestStaleAfterFollowsSchedule(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.RecordCrawlRun(ctx, "vietnamworks", true, "", now)
	s.SetCrawlSchedule(ctx, "vietnamworks", 6*60) // every 6 hours
	if src := source(t, s, now.Add(3*time.Hour), "vietnamworks"); !src.Healthy {
		t.Fatal("a 6-hourly source flagged stalled after 3 hours")
	}
	if src := source(t, s, now.Add(13*time.Hour), "vietnamworks"); src.Healthy {
		t.Fatal("silent for over twice its interval but still healthy")
	}
	got, _ := s.CrawlSchedules(ctx)
	if got["vietnamworks"] != 360 {
		t.Fatalf("schedules = %v", got)
	}
}

// A built-in on its default schedule (nothing stored) is judged by that
// default: careerlink every 2 h isn't stalled 90 minutes after a run.
func TestStaleAfterUsesDefaultSchedule(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.RecordCrawlRun(ctx, "careerlink", true, "", now)
	at := now.Add(90 * time.Minute)
	status := func(defaults map[string]int) bool {
		v, _ := s.CrawlStatus(ctx, at, defaults)
		return v.Sources[0].Healthy
	}
	if status(nil) {
		t.Fatal("with no interval known, 90 minutes of silence should be stale")
	}
	if !status(map[string]int{"careerlink": 120}) {
		t.Fatal("stalled within its 2-hour default interval")
	}
	s.SetCrawlSchedule(ctx, "careerlink", 30) // a stored schedule wins
	if status(map[string]int{"careerlink": 120}) {
		t.Fatal("stored 30-minute schedule ignored")
	}
}

// "Today" is the user's calendar day: at 01:30 on 5 Nov in Vietnam, a job
// saved at 18:00 UTC on 4 Nov (01:00 local on the 5th) counts, and one saved
// at 16:00 UTC (23:00 local on the 4th) doesn't.
func TestCrawlStatusTodayIsLocal(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	ict := time.FixedZone("ICT", 7*3600)
	s.Add(ctx, jobs.Job{Title: "tonight", DiscoverySource: "careerlink"}, time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC))   // 01:00 on the 5th, local
	s.Add(ctx, jobs.Job{Title: "yesterday", DiscoverySource: "careerlink"}, time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)) // 23:00 on the 4th, local
	v, err := s.CrawlStatus(ctx, time.Date(2026, 11, 5, 1, 30, 0, 0, ict), nil)
	if err != nil || v.Date != "2026-11-05" || v.NewJobsToday != 1 {
		t.Fatalf("view = %+v %v, want 2026-11-05 with 1 job", v, err)
	}
}
