package store

// The board's crawl-status payload (its own file so tygo, which writes the
// board's TypeScript types, picks up these and nothing else from crawl.go).

// CrawlStatusView is the board's crawl-widget payload: the day's total, a
// display date, and per-source rows.
type CrawlStatusView struct {
	NewJobsToday int           `json:"newJobsToday"`
	Date         string        `json:"date"`
	Sources      []CrawlSource `json:"sources"`
}

// CrawlSource is one crawler's health plus the jobs it contributed today.
type CrawlSource struct {
	Name      string `json:"name"`
	Count     int    `json:"count"`          // jobs from this source saved today
	Healthy   bool   `json:"healthy"`        // see CrawlStatus
	LastRun   string `json:"lastRun"`        // ISO timestamp of the last run attempt
	LastError string `json:"lastError"`      // why the last run failed ("" if it didn't)
	Task      bool   `json:"task,omitempty"` // a background task (rating, sync), not a job source; set by the service
}
