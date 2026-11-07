package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func TestToday(t *testing.T) {
	s := newSvc(t) // its clock: 2026-11-01 09:00 UTC
	ctx := context.Background()
	if g, err := s.Today(ctx); err != nil || len(g) != 0 {
		t.Fatalf("empty board = %+v %v, want no groups", g, err)
	}
	now := s.now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	for _, j := range []jobs.Job{
		{Title: "B match", Status: "saved", Score: 3.8, HasScore: true, ScoreLetter: "B", ScoreStatus: "scored"},
		{Title: "A match", Status: "waiting", Score: 4.6, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"},
		{Title: "A but applied", Status: "applied", Score: 4.9, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"},
		{Title: "D match", Status: "saved", Score: 2.0, HasScore: true, ScoreLetter: "D", ScoreStatus: "scored"},
		{Title: "Closes today", Status: "saved", ExpiresAt: "expire:" + day(0)},
		{Title: "Closes in 7", Status: "saved", ExpiresAt: "expire:" + day(7)},
		{Title: "Closes in 8", Status: "saved", ExpiresAt: "expire:" + day(8)},
		{Title: "Closed yesterday", Status: "saved", ExpiresAt: "expire:" + day(-1)},
		{Title: "Applied 10 days ago", Status: "applied", ExpiresAt: "applied:" + day(-10)},
		{Title: "Applied 7 days ago", Status: "applied", ExpiresAt: "applied:" + day(-7)},
		{Title: "Applied 6 days ago", Status: "applied", ExpiresAt: "applied:" + day(-6)},
		{Title: "Interviewing", Status: "interview", ExpiresAt: "applied:" + day(-20)},
	} {
		if _, err := s.st.Add(ctx, j, now); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"best_matches": {"A match: A · 4.6", "B match: B · 3.8"},
		"deadlines":    {"Closes today: closes today", "Closes in 7: closes in 7 days"},
		"follow_up":    {"Applied 10 days ago: applied 10 days ago", "Applied 7 days ago: applied 7 days ago"},
	}
	if len(got) != 3 {
		t.Fatalf("groups = %+v", got)
	}
	for i, kind := range []string{"best_matches", "deadlines", "follow_up"} {
		g := got[i]
		if g.Kind != kind || len(g.Items) != len(want[kind]) {
			t.Fatalf("group %d = %+v, want %s %v", i, g, kind, want[kind])
		}
		for k, it := range g.Items {
			if line := it.Title + ": " + it.Why; line != want[kind][k] || it.JobID == 0 {
				t.Errorf("%s[%d] = %q (id %d), want %q", kind, k, line, it.JobID, want[kind][k])
			}
		}
	}
}

// Asking the company resets the follow-up clock: Today asks again only after
// another week without an answer.
func TestTodayFollowUpCountsFromTheLastAsk(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	now := s.now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	asked := func(d int) string { return now.AddDate(0, 0, d).UTC().Format(time.RFC3339) }
	for _, j := range []jobs.Job{
		{Title: "Asked 3 days ago", Status: "applied", ExpiresAt: "applied:" + day(-20), FollowupSentAt: asked(-3)},
		{Title: "Asked 9 days ago", Status: "applied", ExpiresAt: "applied:" + day(-20), FollowupSentAt: asked(-9)},
	} {
		if _, err := s.st.Add(ctx, j, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Today(ctx)
	if err != nil || len(got) != 1 || len(got[0].Items) != 1 {
		t.Fatalf("Today = %+v %v, want one follow-up", got, err)
	}
	if it := got[0].Items[0]; it.Title != "Asked 9 days ago" || it.Why != "followed up 9 days ago" {
		t.Errorf("follow-up = %+v", it)
	}
}

func TestMarkFollowedUp(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	applied, err := s.st.Add(ctx, jobs.Job{Title: "Waiting", Status: "applied", ExpiresAt: "applied:2026-10-20"}, s.now())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.st.Add(ctx, jobs.Job{Title: "Not applied", Status: "saved"}, s.now())
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.MarkFollowedUp(ctx, applied)
	if err != nil || j.FollowupSentAt != s.now().UTC().Format(time.RFC3339) {
		t.Fatalf("MarkFollowedUp = %q, %v", j.FollowupSentAt, err)
	}
	if g, _ := s.Today(ctx); len(g) != 0 {
		t.Errorf("Today still asks for a follow-up: %+v", g)
	}
	if _, err := s.MarkFollowedUp(ctx, saved); !errors.Is(err, ErrInvalid) {
		t.Errorf("following up a saved job: %v, want ErrInvalid", err)
	}
}

func TestTodayCapsEachGroup(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		s.st.Add(ctx, jobs.Job{Title: "A", Status: "saved", Score: 4 + float64(i)/10, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"}, time.Now())
	}
	g, _ := s.Today(ctx)
	if len(g) != 1 || len(g[0].Items) != todayPerGroup || g[0].Items[0].Why != "A · 4.4" {
		t.Fatalf("groups = %+v, want the top %d, best first", g, todayPerGroup)
	}
}
