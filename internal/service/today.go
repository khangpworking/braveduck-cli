package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// Today is what to act on now, the same on the board, the CLI and MCP: the
// best-rated jobs not applied to yet, postings that close this week, and
// applications still waiting for an answer after a week.

// TodayItem is one job to act on and why.
type TodayItem struct {
	JobID int64  `json:"job_id"`
	Title string `json:"title"`
	Why   string `json:"why"`
}

// TodayGroup is one kind of action; empty groups are left out.
type TodayGroup struct {
	Kind  string      `json:"kind"` // best_matches, deadlines, follow_up
	Title string      `json:"title"`
	Items []TodayItem `json:"items"`
}

// Today's limits: a few items per group, deadlines within a week, follow-ups
// after a week of silence.
const (
	todayPerGroup     = 3
	todayDeadlineDays = 7
	todayFollowUpDays = 7
)

// Today returns the groups that have something in them, in a fixed order.
func (s *Service) Today(ctx context.Context) ([]TodayGroup, error) {
	all, err := s.st.List(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	return todayGroups(all, s.now()), nil
}

func todayGroups(all []jobs.Job, now time.Time) []TodayGroup {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := func(iso string) (int, bool) {
		t, err := time.ParseInLocation("2006-01-02", iso, now.Location())
		if err != nil {
			return 0, false
		}
		return int(math.Round(t.Sub(today).Hours() / 24)), true // DST days aren't 24 h
	}
	open := func(j jobs.Job) bool { return j.Status == "saved" || j.Status == "waiting" }

	type ranked struct {
		job jobs.Job
		key float64 // sort descending
		why string
	}
	var best, deadlines, followUps []ranked
	for _, j := range all {
		if open(j) && j.ScoreStatus == "scored" && (j.ScoreLetter == "A" || j.ScoreLetter == "B") {
			best = append(best, ranked{job: j, key: j.Score, why: fmt.Sprintf("%s · %.1f", j.ScoreLetter, j.Score)})
		}
		e := jobs.ParseExpiry(j.ExpiresAt)
		switch {
		case e.Kind == jobs.KindDeadline && open(j):
			if d, ok := days(e.Date); ok && d >= 0 && d <= todayDeadlineDays {
				deadlines = append(deadlines, ranked{job: j, key: -float64(d), why: closesIn(d)})
			}
		case e.Kind == jobs.KindApplied && j.Status == "applied":
			// Counted from the last time the user asked, if they have.
			d, ok := days(e.Date)
			why := "applied %d days ago"
			if t, err := time.Parse(time.RFC3339, j.FollowupSentAt); err == nil {
				d, ok = days(t.In(now.Location()).Format("2006-01-02"))
				why = "followed up %d days ago"
			}
			if ok && -d >= todayFollowUpDays {
				followUps = append(followUps, ranked{job: j, key: float64(-d), why: fmt.Sprintf(why, -d)})
			}
		}
	}

	group := func(kind, title string, rs []ranked) *TodayGroup {
		if len(rs) == 0 {
			return nil
		}
		sort.SliceStable(rs, func(a, b int) bool { return rs[a].key > rs[b].key })
		g := &TodayGroup{Kind: kind, Title: title}
		for i, r := range rs {
			if i == todayPerGroup {
				break
			}
			g.Items = append(g.Items, TodayItem{JobID: r.job.ID, Title: r.job.Title, Why: r.why})
		}
		return g
	}
	out := []TodayGroup{}
	for _, g := range []*TodayGroup{
		group("best_matches", "Best matches to apply for", best),
		group("deadlines", "Deadlines this week", deadlines),
		group("follow_up", "Follow up", followUps),
	} {
		if g != nil {
			out = append(out, *g)
		}
	}
	return out
}

func closesIn(d int) string {
	switch d {
	case 0:
		return "closes today"
	case 1:
		return "closes tomorrow"
	}
	return fmt.Sprintf("closes in %d days", d)
}
