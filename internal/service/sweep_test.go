package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func TestSweepExpiredMovesToClosed(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	now := s.now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	ids := map[string]int64{}
	for _, j := range []jobs.Job{
		{Title: "closed 8 days ago", Status: "saved", ExpiresAt: "expire:" + day(-8), Notes: "my note"},
		{Title: "closed 6 days ago", Status: "saved", ExpiresAt: "expire:" + day(-6)},
		{Title: "waiting, closed 30 days ago", Status: "waiting", ExpiresAt: "expire:" + day(-30)},
		{Title: "pinned, closed long ago", Status: "saved", ExpiresAt: "expire:" + day(-60), Pinned: true},
		{Title: "kept, closed long ago", Status: "saved", ExpiresAt: "expire:" + day(-60), Keep: true},
		{Title: "applied 28 days ago", Status: "applied", ExpiresAt: "applied:" + day(-28)},
		{Title: "applied 26 days ago", Status: "applied", ExpiresAt: "applied:" + day(-26)},
		{Title: "interview, applied long ago", Status: "interview", ExpiresAt: "applied:" + day(-90)},
		{Title: "no deadline", Status: "saved"},
	} {
		id, err := s.st.Add(ctx, j, now)
		if err != nil {
			t.Fatal(err)
		}
		ids[j.Title] = id
	}

	moved, err := s.SweepExpired(ctx)
	if err != nil || moved != 3 {
		t.Fatalf("moved %d (%v), want 3", moved, err)
	}
	wantClosed := map[string]string{
		"closed 8 days ago":           "the posting closed on " + day(-8),
		"waiting, closed 30 days ago": "the posting closed on " + day(-30),
		"applied 28 days ago":         "no answer 28 days after applying",
	}
	for title, id := range ids {
		j, _ := s.GetJob(ctx, id)
		why, should := wantClosed[title]
		if should != (j.Status == "closed") {
			t.Errorf("%q: status %q", title, j.Status)
		}
		if should && !strings.Contains(j.Notes, why) {
			t.Errorf("%q: notes %q, want %q", title, j.Notes, why)
		}
	}
	if j, _ := s.GetJob(ctx, ids["closed 8 days ago"]); !strings.HasSuffix(j.Notes, "\nmy note") {
		t.Errorf("the user's note was lost: %q", j.Notes)
	}
	if ev, _ := s.Activity(ctx, 1); len(ev) != 1 || !strings.Contains(ev[0].Message, "Moved 3 expired job(s) to Closed") {
		t.Errorf("activity = %+v", ev)
	}

	// Already closed: nothing left to move, and nothing is deleted.
	if moved, _ := s.SweepExpired(ctx); moved != 0 {
		t.Fatalf("second sweep moved %d", moved)
	}
	if list, _ := s.ListJobs(ctx, ListFilter{}); len(list) != len(ids) {
		t.Fatalf("%d jobs left, want all %d (the sweep never deletes)", len(list), len(ids))
	}
}

func TestSweepIfDueRunsHourly(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	now := s.now()
	s.st.Add(ctx, jobs.Job{Title: "old", Status: "saved", ExpiresAt: "expire:" + now.AddDate(0, 0, -10).Format("2006-01-02")}, now)

	last := now.Add(-30 * time.Minute)
	s.sweepIfDue(ctx, &last)
	if list, _ := s.ListJobs(ctx, ListFilter{Status: "closed"}); len(list) != 0 {
		t.Fatal("swept before an hour passed")
	}
	last = now.Add(-2 * time.Hour)
	s.sweepIfDue(ctx, &last)
	if list, _ := s.ListJobs(ctx, ListFilter{Status: "closed"}); len(list) != 1 || !last.Equal(now) {
		t.Fatalf("closed = %d, last = %v, want the sweep to run and record now", len(list), last)
	}
}

// Dragging a swept card back out of Closed keeps it, so the next sweep
// doesn't close it again.
func TestSweptCardDraggedBackStays(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	id, _ := s.st.Add(ctx, jobs.Job{Title: "old", Status: "saved", ExpiresAt: "expire:" + s.now().AddDate(0, 0, -10).Format("2006-01-02")}, s.now())
	if n, _ := s.SweepExpired(ctx); n != 1 {
		t.Fatalf("first sweep moved %d, want 1", n)
	}
	j, err := s.SetStatus(ctx, id, "saved")
	if err != nil || !j.Keep {
		t.Fatalf("dragged back: %v keep=%v, want kept", err, j.Keep)
	}
	if n, _ := s.SweepExpired(ctx); n != 0 {
		t.Fatalf("second sweep moved %d, want the kept card left alone", n)
	}
	// A card closed by hand isn't marked kept when reopened.
	m, _ := s.AddJob(ctx, NewJob{Title: "by hand"})
	s.SetStatus(ctx, m.ID, "closed")
	if j, _ := s.SetStatus(ctx, m.ID, "saved"); j.Keep {
		t.Fatal("reopening a hand-closed card marked it kept")
	}
}
