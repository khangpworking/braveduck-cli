package store

import (
	"context"
	"database/sql"
	"time"
)

// StaleAfter is the shortest silence after which the board calls a reported
// crawler stalled. A source with a schedule gets twice its interval instead,
// if that is longer (a 6-hourly source isn't stalled after 61 minutes).
const StaleAfter = time.Hour

// Schedule is how often one source fetches new jobs; 0 means off.
type Schedule struct {
	Source       string
	EveryMinutes int
}

// CrawlSchedules returns every stored schedule, by source.
func (s *Store) CrawlSchedules(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT source, every_minutes FROM crawl_schedule")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var src string
		var n int
		if err := rows.Scan(&src, &n); err != nil {
			return nil, err
		}
		out[src] = n
	}
	return out, rows.Err()
}

// SetCrawlSchedule stores how often source fetches new jobs (0 = off).
func (s *Store) SetCrawlSchedule(ctx context.Context, source string, everyMinutes int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO crawl_schedule (source, every_minutes) VALUES (?, ?)
		 ON CONFLICT(source) DO UPDATE SET every_minutes = excluded.every_minutes`,
		source, everyMinutes)
	return err
}

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

// CrawlStatus reports per-source crawl health from crawl_runs, joined with the
// count of jobs each source saved today. "Today" is now's calendar day in
// now's time zone (saved_at is UTC, so the day's bounds are converted).
// Sources with no crawl_runs row simply don't appear; NewJobsToday still
// counts every job saved today, source-tagged or not.
//
// A source is healthy when its last run succeeded within StaleAfter, or twice
// its interval if that's longer; a source switched off (every 0 minutes) is
// healthy unless its last run failed. The interval is its stored schedule, else
// defaultEvery[name] (a built-in on its default schedule), in minutes. Rows
// from before crawl-report existed (no last_success_at) fall back to "last
// succeeded today".
func (s *Store) CrawlStatus(ctx context.Context, now time.Time, defaultEvery map[string]int) (CrawlStatusView, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today := start.Format("2006-01-02")
	from, to := start.UTC().Format(time.RFC3339), start.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	todayUTC := now.UTC().Format("2006-01-02") // last_success_date is a UTC date

	// Per-source count of jobs saved today (saved_at is a UTC RFC3339 string).
	counts := map[string]int{}
	total := 0
	rows, err := s.db.QueryContext(ctx,
		"SELECT COALESCE(discovery_source,''), COUNT(*) FROM jobs WHERE saved_at >= ? AND saved_at < ? GROUP BY discovery_source",
		from, to)
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
		`SELECT r.name, COALESCE(r.last_success_date,''), COALESCE(r.last_run_at,''),
		        COALESCE(r.last_success_at,''), r.last_ok, COALESCE(r.last_error,''),
		        c.every_minutes
		 FROM crawl_runs r LEFT JOIN crawl_schedule c ON c.source = r.name
		 ORDER BY r.name`)
	if err != nil {
		return CrawlStatusView{}, err
	}
	defer crawlRows.Close()

	sources := []CrawlSource{}
	for crawlRows.Next() {
		var name, lastSuccess, lastRun, successAt, lastErr string
		var lastOK int
		var stored sql.NullInt64
		if err := crawlRows.Scan(&name, &lastSuccess, &lastRun, &successAt, &lastOK, &lastErr, &stored); err != nil {
			return CrawlStatusView{}, err
		}
		every := defaultEvery[name]
		if stored.Valid {
			every = int(stored.Int64)
		}
		stale := StaleAfter
		if d := 2 * time.Duration(every) * time.Minute; d > stale {
			stale = d
		}
		healthy := lastSuccess == todayUTC
		if successAt != "" {
			t, err := time.Parse(time.RFC3339, successAt)
			healthy = err == nil && lastOK == 1 && now.Sub(t) <= stale
		}
		if stored.Valid && stored.Int64 == 0 {
			// Switched off: not running is expected, only a failed last run counts.
			healthy = lastOK == 1
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
