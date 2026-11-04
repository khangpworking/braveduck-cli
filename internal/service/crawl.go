package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
)

// Crawling: braveduck's own crawlers (VietnamWorks…) fetch new jobs for the
// user's search queries, drop what their keyword filters reject, and add the
// rest to the board. One schedule setting covers every source: built-in
// crawlers run on it while the board is open; external tools (fb-watcher)
// read their own setting with `braveduck crawl-schedule <source>`.

// Schedule bounds, in minutes. Built-in crawlers hit someone else's server, so
// they get a politer floor than an external tool polling its own groups.
const (
	MinEveryMinutes        = 5
	MinBuiltinEveryMinutes = 15
	MaxEveryMinutes        = 24 * 60
	DefaultEveryMinutes    = 120
)

// ErrNoQueries means there is nothing to search for yet.
var ErrNoQueries = errors.New("no search queries yet — add some: braveduck keywords add search_queries \"ai automation\" \"n8n\"")

// ErrCrawlRunning means that source is already being crawled.
var ErrCrawlRunning = errors.New("that source is already being crawled")

// crawlState is the service's crawler machinery.
type crawlState struct {
	crawlers map[string]crawl.Crawler
	mu       sync.Mutex
	running  map[string]bool
}

// SetCrawlers replaces the built-in crawlers (tests use fakes).
func (s *Service) SetCrawlers(cs ...crawl.Crawler) {
	m := map[string]crawl.Crawler{}
	for _, c := range cs {
		m[c.Name()] = c
	}
	s.crawl = &crawlState{crawlers: m, running: map[string]bool{}}
}

func (s *Service) crawlers() *crawlState {
	if s.crawl == nil {
		s.SetCrawlers(crawl.Builtin()...)
	}
	return s.crawl
}

// CrawlResult is what one crawl did.
type CrawlResult struct {
	Source   string `json:"source"`
	Queries  int    `json:"queries"`
	Found    int    `json:"found"`    // listings the source returned
	Filtered int    `json:"filtered"` // dropped by the keyword filters
	Added    int    `json:"added"`    // new on the board
}

// Crawl runs one built-in crawler now: every search query, keyword filters
// applied, duplicates skipped. The run is reported to the crawl panel either way.
func (s *Service) Crawl(ctx context.Context, source string) (CrawlResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	cs := s.crawlers()
	c, ok := cs.crawlers[source]
	if !ok {
		return CrawlResult{}, fmt.Errorf("%w: %q isn't a built-in crawler (built-in: %s)", ErrInvalid, source, strings.Join(s.BuiltinCrawlers(), ", "))
	}
	cs.mu.Lock()
	if cs.running[source] {
		cs.mu.Unlock()
		return CrawlResult{}, ErrCrawlRunning
	}
	cs.running[source] = true
	cs.mu.Unlock()
	defer func() { cs.mu.Lock(); delete(cs.running, source); cs.mu.Unlock() }()

	res, err := s.crawlOnce(ctx, c)
	if err != nil {
		_ = s.ReportCrawl(ctx, source, false, err.Error())
		return res, err
	}
	if err := s.ReportCrawl(ctx, source, true, ""); err != nil {
		return res, err
	}
	if res.Added > 0 {
		s.logEvent(ctx, "added", fmt.Sprintf("%s: %d new job(s)", source, res.Added))
	}
	return res, nil
}

func (s *Service) crawlOnce(ctx context.Context, c crawl.Crawler) (CrawlResult, error) {
	res := CrawlResult{Source: c.Name()}
	sets, err := s.st.AllKeywords(ctx)
	if err != nil {
		return res, err
	}
	queries := sets["search_queries"]
	if len(queries) == 0 {
		return res, ErrNoQueries
	}
	res.Queries = len(queries)
	seen := map[string]bool{}
	var firstErr error
	for _, q := range queries {
		ls, err := c.Search(ctx, q)
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("query %q: %w", q, err)
			}
			continue
		}
		for _, l := range ls {
			if seen[l.URL] {
				continue
			}
			seen[l.URL] = true
			res.Found++
			if d := keyword.Filter(keyword.Job{Title: l.Title, Description: l.Description, Company: l.Company}, sets); !d.Keep {
				res.Filtered++
				continue
			}
			if dup, err := s.st.ExistsByURL(ctx, l.URL); err != nil || dup {
				continue
			}
			j := jobs.Job{
				Title: l.Title, Company: l.Company, URL: l.URL, Location: l.Location,
				Salary: l.Salary, Skills: strings.Join(l.Skills, ", "), FullJD: l.Description,
				ContactEmail: l.Email, DiscoverySource: c.Name(),
			}
			if l.Deadline != "" {
				j.ExpiresAt = jobs.ExpirePrefix + l.Deadline
			}
			if _, err := s.addQuiet(ctx, j); err == nil {
				res.Added++
			}
		}
	}
	// Every query failing is a failed run; a partial failure still counts as
	// a run (the board shows the jobs it did get).
	if firstErr != nil && res.Found == 0 {
		return res, firstErr
	}
	return res, nil
}

// BuiltinCrawlers lists braveduck's own crawler ids, sorted.
func (s *Service) BuiltinCrawlers() []string {
	var ids []string
	for id := range s.crawlers().crawlers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SourceSchedule is one source's fetch setting.
type SourceSchedule struct {
	Source       string `json:"source"`
	EveryMinutes int    `json:"every_minutes"` // 0 = off
	Builtin      bool   `json:"builtin"`       // braveduck runs it; else an external tool reads the setting
	Configured   bool   `json:"configured"`    // false = still on the default
}

// CrawlSchedules lists every known source: the built-in crawlers, plus any
// source that has reported a run or has a stored setting.
func (s *Service) CrawlSchedules(ctx context.Context) ([]SourceSchedule, error) {
	stored, err := s.st.CrawlSchedules(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, id := range s.BuiltinCrawlers() {
		known[id] = true
	}
	if v, err := s.st.CrawlStatus(ctx, s.now()); err == nil {
		for _, src := range v.Sources {
			known[src.Name] = true
		}
	}
	for src := range stored {
		known[src] = true
	}
	builtin := s.crawlers().crawlers
	var out []SourceSchedule
	for src := range known {
		_, isBuiltin := builtin[src]
		every, configured := stored[src]
		if !configured && isBuiltin {
			every = DefaultEveryMinutes
		}
		out = append(out, SourceSchedule{Source: src, EveryMinutes: every, Builtin: isBuiltin, Configured: configured})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out, nil
}

// SetCrawlSchedule sets how often source fetches new jobs; 0 turns it off.
func (s *Service) SetCrawlSchedule(ctx context.Context, source string, everyMinutes int) (SourceSchedule, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if !crawlSourceRE.MatchString(source) {
		return SourceSchedule{}, fmt.Errorf("%w: source %q: use lowercase letters, digits, - or _", ErrInvalid, source)
	}
	_, isBuiltin := s.crawlers().crawlers[source]
	min := MinEveryMinutes
	if isBuiltin {
		min = MinBuiltinEveryMinutes
	}
	if everyMinutes != 0 && (everyMinutes < min || everyMinutes > MaxEveryMinutes) {
		return SourceSchedule{}, fmt.Errorf("%w: every %d min — use 0 (off) or %d to %d minutes", ErrInvalid, everyMinutes, min, MaxEveryMinutes)
	}
	if err := s.st.SetCrawlSchedule(ctx, source, everyMinutes); err != nil {
		return SourceSchedule{}, err
	}
	return SourceSchedule{Source: source, EveryMinutes: everyMinutes, Builtin: isBuiltin, Configured: true}, nil
}

// RunScheduler crawls each built-in source when its interval has passed,
// checking once a minute, until ctx ends. `braveduck serve` runs it. Failures
// are reported to the crawl panel; they never stop the loop.
func (s *Service) RunScheduler(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		s.runDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) runDue(ctx context.Context) {
	scheds, err := s.CrawlSchedules(ctx)
	if err != nil {
		return
	}
	last := map[string]time.Time{}
	if v, err := s.st.CrawlStatus(ctx, s.now()); err == nil {
		for _, src := range v.Sources {
			if t, err := time.Parse(time.RFC3339, src.LastRun); err == nil {
				last[src.Name] = t
			}
		}
	}
	for _, sc := range scheds {
		if !sc.Builtin || sc.EveryMinutes == 0 {
			continue
		}
		if t, ok := last[sc.Source]; ok && s.now().Sub(t) < time.Duration(sc.EveryMinutes)*time.Minute {
			continue
		}
		_, _ = s.Crawl(ctx, sc.Source) // the outcome is in the crawl panel
	}
}
