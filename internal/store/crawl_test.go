package store

import (
	"context"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func source(t *testing.T, s *Store, at time.Time, name string) CrawlSource {
	t.Helper()
	v, err := s.CrawlStatus(context.Background(), at)
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
