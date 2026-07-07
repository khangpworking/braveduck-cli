package store

import (
	"context"
	"time"
)

// CrawlSource is one crawler's health plus the jobs it contributed today.
type CrawlSource struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`   // jobs from this source saved today
	Healthy bool   `json:"healthy"` // last run succeeded today
	LastRun string `json:"lastRun"` // ISO timestamp of the last run attempt
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
		"SELECT name, COALESCE(last_success_date,''), COALESCE(last_run_at,'') FROM crawl_runs ORDER BY name")
	if err != nil {
		return CrawlStatusView{}, err
	}
	defer crawlRows.Close()

	sources := []CrawlSource{}
	for crawlRows.Next() {
		var name, lastSuccess, lastRun string
		if err := crawlRows.Scan(&name, &lastSuccess, &lastRun); err != nil {
			return CrawlStatusView{}, err
		}
		sources = append(sources, CrawlSource{
			Name:    name,
			Count:   counts[name],
			Healthy: lastSuccess == today,
			LastRun: lastRun,
		})
	}
	if err := crawlRows.Err(); err != nil {
		return CrawlStatusView{}, err
	}

	return CrawlStatusView{NewJobsToday: total, Date: today, Sources: sources}, nil
}
