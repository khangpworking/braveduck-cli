package store

import (
	"context"
	"time"
)

// CrawlSource is one crawler's health plus the jobs it contributed today.
type CrawlSource struct {
	Name      string `json:"name"`
	Count     int    `json:"count"`     // jobs from this source saved today
	Healthy   bool   `json:"healthy"`   // see CrawlStatus
	LastRun   string `json:"lastRun"`   // ISO timestamp of the last run attempt
	LastError string `json:"lastError"` // why the last run failed ("" if it didn't)
}

// StaleAfter is how long a reported crawler may stay silent before the board
// calls it stalled. External watchers report every few minutes; one that has
// crashed stops reporting.
const StaleAfter = time.Hour

// RecordCrawlRun stores one run of an external crawler. A failed run keeps
// the last success time, so the board can say how long it has been down.
func (s *Store) RecordCrawlRun(ctx context.Context, name string, ok bool, errMsg string, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	var successAt, successDate any
	if ok {
		successAt, successDate, errMsg = ts, now.UTC().Format("2006-01-02"), ""
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO crawl_runs (name, last_run_at, last_ok, last_error, last_success_at, last_success_date)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   last_run_at = excluded.last_run_at, last_ok = excluded.last_ok,
		   last_error = excluded.last_error,
		   last_success_at = COALESCE(excluded.last_success_at, crawl_runs.last_success_at),
		   last_success_date = COALESCE(excluded.last_success_date, crawl_runs.last_success_date)`,
		name, ts, b2i(ok), errMsg, successAt, successDate)
	return err
}

// CrawlStatusView is the board's crawl-widget payload: the day's total, a
// display date, and per-source rows.
type CrawlStatusView struct {
	NewJobsToday int           `json:"newJobsToday"`
	Date         string        `json:"date"`
	Sources      []CrawlSource `json:"sources"`
}

// CrawlStatus reports per-source crawl health from crawl_runs, joined with the
// count of jobs each source saved today. "Today" is now's UTC date, matching
// how saved_at is stored. Sources with no crawl_runs row simply don't appear;
// NewJobsToday still counts every job saved today, source-tagged or not.
//
// A source is healthy when its last run succeeded within StaleAfter. Rows
// from before crawl-report existed (no last_success_at) fall back to "last
// succeeded today".
func (s *Store) CrawlStatus(ctx context.Context, now time.Time) (CrawlStatusView, error) {
	today := now.UTC().Format("2006-01-02")

	// Per-source count of jobs saved today (saved_at is a UTC RFC3339 string).
	counts := map[string]int{}
	total := 0
	rows, err := s.db.QueryContext(ctx,
		"SELECT COALESCE(discovery_source,''), COUNT(*) FROM jobs WHERE substr(saved_at,1,10) = ? GROUP BY discovery_source",
		today)
	if err != nil {
		return CrawlStatusView{}, err
	}
	for rows.Next() {
		var src string
		var n int
		if err := rows.Scan(&src, &n); err != nil {
			rows.Close()
			return CrawlStatusView{}, err
		}
		counts[src] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return CrawlStatusView{}, err
	}
	rows.Close()

	// One row per registered crawler; healthy == it last succeeded today.
	crawlRows, err := s.db.QueryContext(ctx,
		`SELECT name, COALESCE(last_success_date,''), COALESCE(last_run_at,''),
		        COALESCE(last_success_at,''), last_ok, COALESCE(last_error,'')
		 FROM crawl_runs ORDER BY name`)
	if err != nil {
		return CrawlStatusView{}, err
	}
	defer crawlRows.Close()

	sources := []CrawlSource{}
	for crawlRows.Next() {
		var name, lastSuccess, lastRun, successAt, lastErr string
		var lastOK int
		if err := crawlRows.Scan(&name, &lastSuccess, &lastRun, &successAt, &lastOK, &lastErr); err != nil {
			return CrawlStatusView{}, err
		}
		healthy := lastSuccess == today
		if successAt != "" {
			t, err := time.Parse(time.RFC3339, successAt)
			healthy = err == nil && lastOK == 1 && now.Sub(t) <= StaleAfter
		}
		sources = append(sources, CrawlSource{
			Name:      name,
			Count:     counts[name],
			Healthy:   healthy,
			LastRun:   lastRun,
			LastError: lastErr,
		})
	}
	if err := crawlRows.Err(); err != nil {
		return CrawlStatusView{}, err
	}

	return CrawlStatusView{NewJobsToday: total, Date: today, Sources: sources}, nil
}
