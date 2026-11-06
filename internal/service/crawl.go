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
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
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
var ErrNoQueries = errors.New("no search terms yet — add some under Settings → Search terms & filters, or: braveduck keywords add search_queries \"ai automation\" \"n8n\"")

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
	crawlInitMu.Lock()
	defer crawlInitMu.Unlock()
	s.setCrawlersLocked(cs...)
}

func (s *Service) setCrawlersLocked(cs ...crawl.Crawler) {
	m := map[string]crawl.Crawler{}
	for _, c := range cs {
		m[c.Name()] = c
	}
	s.crawl = &crawlState{crawlers: m, running: map[string]bool{}}
}

func (s *Service) crawlers() *crawlState {
	crawlInitMu.Lock() // the scheduler and the board can get here at once
	defer crawlInitMu.Unlock()
	if s.crawl == nil {
		s.setCrawlersLocked(crawl.Builtin()...)
	}
	return s.crawl
}

// crawlInitMu guards setting up s.crawl.
var crawlInitMu sync.Mutex

// CrawlResult is what one crawl did.
type CrawlResult struct {
	Source     string `json:"source"`
	Queries    int    `json:"queries"`
	Found      int    `json:"found"`      // listings the source returned
	Filtered   int    `json:"filtered"`   // dropped by the keyword filters
	Duplicates int    `json:"duplicates"` // already on the board (pasted, synced or crawled before)
	Added      int    `json:"added"`      // new on the board
}

// TopCVSyncSource is the schedule/crawl-panel id of the TopCV status sync, a
// built-in task alongside the crawlers (default every 6 h, only while the user
// is logged in to TopCV).
const TopCVSyncSource = "topcv-sync"

const defaultSyncEveryMinutes = 360

// Crawl runs one built-in source now: a crawler (every search query, keyword
// filters applied, duplicates skipped) or the TopCV status sync. The run is
// reported to the crawl panel either way.
func (s *Service) Crawl(ctx context.Context, source string) (CrawlResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	cs := s.crawlers()
	if source == TopCVSyncSource {
		r, err := s.SyncTopCV(ctx)
		return CrawlResult{Source: source, Found: r.Found, Added: r.Added}, err
	}
	if source == JevRateSource {
		r, err := s.runJevRate(ctx)
		if err != nil {
			_ = s.ReportCrawl(ctx, source, false, err.Error())
		} else if s.jevIsDefault(ctx) {
			_ = s.ReportCrawl(ctx, source, true, "")
		}
		return r, err
	}
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

	release, err := takeSiteTurn(ctx, source)
	if err != nil {
		return CrawlResult{Source: source}, err
	}
	cctx, cancel := context.WithTimeout(ctx, maxCrawlTime)
	res, err := s.crawlOnce(cctx, c)
	cancel()
	release()
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

// maxCrawlTime bounds one crawl, so no crawl keeps its site's turn for long.
const maxCrawlTime = 15 * time.Minute

// HostedMinEveryMinutes is the shortest crawl interval on a shared server.
const HostedMinEveryMinutes = 60

// siteTurns lets one crawl of each job site run at a time across the whole
// process: on a shared server every user's service crawls the same sites,
// and the sites should see one polite crawler, not one per user.
var siteTurns sync.Map // source → chan struct{} (capacity 1)

// takeSiteTurn waits until no other crawl of source runs in this process;
// the returned func ends the turn.
func takeSiteTurn(ctx context.Context, source string) (func(), error) {
	v, _ := siteTurns.LoadOrStore(source, make(chan struct{}, 1))
	turn := v.(chan struct{})
	select {
	case turn <- struct{}{}:
		return func() { <-turn }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
	board, err := s.loadDupIndex(ctx)
	if err != nil {
		return res, err
	}
	if ks, ok := c.(crawl.KnownSkipper); ok {
		ks.SkipKnown(func(u string) bool { return len(board.find(jobs.Job{URL: u})) > 0 })
	}
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
			j := jobs.Job{
				Title: l.Title, Company: l.Company, URL: l.URL, Location: l.Location,
				Salary: l.Salary, Skills: strings.Join(l.Skills, ", "), FullJD: l.Description,
				ContactEmail: l.Email, DiscoverySource: c.Name(),
			}
			if l.Deadline != "" {
				j.ExpiresAt = jobs.ExpirePrefix + l.Deadline
			}
			if len(board.find(j)) > 0 {
				res.Duplicates++
				continue
			}
			if saved, err := s.addQuiet(ctx, j); err == nil {
				board.add(saved)
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

// builtinSources are the sources braveduck runs itself: crawlers + the sync.
func (s *Service) builtinSources() map[string]int {
	m := map[string]int{TopCVSyncSource: defaultSyncEveryMinutes, JevRateSource: defaultJevEveryMinutes}
	for id := range s.crawlers().crawlers {
		m[id] = DefaultEveryMinutes
	}
	return m
}

// SourceSchedule is one source's fetch setting.
type SourceSchedule struct {
	Source          string `json:"source"`
	Name            string `json:"name"`              // what the board calls it
	EveryMinutes    int    `json:"every_minutes"`     // 0 = off
	MinEveryMinutes int    `json:"min_every_minutes"` // the shortest interval allowed (other than off)
	Builtin         bool   `json:"builtin"`           // braveduck runs it; else an external tool reads the setting
	Task            bool   `json:"task"`              // a background task (rating, sync), not a job site
	Configured      bool   `json:"configured"`        // false = still on the default
}

// isTaskSource reports whether source is a background task, not a job site.
func isTaskSource(source string) bool {
	return source == TopCVSyncSource || source == JevRateSource
}

// sourceName is a source's name for people: the site's own name when it's a
// site braveduck knows, else its id (an external tool names itself).
func sourceName(source string) string {
	switch source {
	case TopCVSyncSource:
		return "TopCV application updates"
	case JevRateSource:
		return "Automatic job rating"
	}
	for _, site := range scrape.Sources {
		if site.ID == source {
			return site.Name
		}
	}
	return source
}

// minEveryMinutes is the shortest interval a source may be set to.
func (s *Service) minEveryMinutes(isBuiltin bool) int {
	switch {
	case !isBuiltin:
		return MinEveryMinutes
	case s.hosted != nil:
		return HostedMinEveryMinutes // every user crawls the same sites, taking turns
	default:
		return MinBuiltinEveryMinutes
	}
}

func (s *Service) sourceSchedule(source string, every int, isBuiltin, configured bool) SourceSchedule {
	return SourceSchedule{Source: source, Name: sourceName(source), EveryMinutes: every, MinEveryMinutes: s.minEveryMinutes(isBuiltin),
		Builtin: isBuiltin, Task: isTaskSource(source), Configured: configured}
}

// CrawlSchedules lists every known source: the built-in crawlers, plus any
// source that has reported a run or has a stored setting.
func (s *Service) CrawlSchedules(ctx context.Context) ([]SourceSchedule, error) {
	stored, err := s.st.CrawlSchedules(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	builtin := s.builtinSources()
	for id := range builtin {
		known[id] = true
	}
	if v, err := s.st.CrawlStatus(ctx, s.now(), nil); err == nil {
		for _, src := range v.Sources {
			known[src.Name] = true
		}
	}
	for src := range stored {
		known[src] = true
	}
	var out []SourceSchedule
	for src := range known {
		def, isBuiltin := builtin[src]
		every, configured := stored[src]
		if !configured && isBuiltin {
			every = def
		}
		out = append(out, s.sourceSchedule(src, every, isBuiltin, configured))
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
	_, isBuiltin := s.builtinSources()[source]
	min := s.minEveryMinutes(isBuiltin)
	if everyMinutes != 0 && (everyMinutes < min || everyMinutes > MaxEveryMinutes) {
		return SourceSchedule{}, fmt.Errorf("%w: every %d min — use 0 (off) or %d to %d minutes", ErrInvalid, everyMinutes, min, MaxEveryMinutes)
	}
	if err := s.st.SetCrawlSchedule(ctx, source, everyMinutes); err != nil {
		return SourceSchedule{}, err
	}
	return s.sourceSchedule(source, everyMinutes, isBuiltin, true), nil
}

// RunScheduler crawls each built-in source when its interval has passed,
// checking once a minute, moves expired jobs to Closed once an hour
// (SweepExpired) and backs the database up once a day (Backup), until ctx
// ends. `braveduck serve` runs it. Failures are
// reported to the crawl panel; they never stop the loop.
func (s *Service) RunScheduler(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	var lastSweep time.Time
	for {
		s.runDue(ctx)
		s.sweepIfDue(ctx, &lastSweep)
		s.backupIfDue(ctx)
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
	if v, err := s.st.CrawlStatus(ctx, s.now(), nil); err == nil {
		for _, src := range v.Sources {
			if t, err := time.Parse(time.RFC3339, src.LastRun); err == nil {
				last[src.Name] = t
			}
		}
	}
	// Crawlers have nothing to search for until the user adds search terms;
	// Get started asks for them, so don't flag every crawl as failing.
	noTerms := false
	if sets, err := s.st.AllKeywords(ctx); err == nil {
		noTerms = len(sets["search_queries"]) == 0
	}
	for _, sc := range scheds {
		if !sc.Builtin || sc.EveryMinutes == 0 {
			continue
		}
		if noTerms && sc.Source != TopCVSyncSource && sc.Source != JevRateSource {
			continue
		}
		if sc.Source == TopCVSyncSource && !s.platformConnected(ctx, "topcv") {
			continue // nothing to sync without a login; don't flag it as failing
		}
		if sc.Source == JevRateSource && !s.autoEnableJev(ctx) {
			continue // no Jev key, or the user picked another rater
		}
		if t, ok := last[sc.Source]; ok && s.now().Sub(t) < time.Duration(sc.EveryMinutes)*time.Minute {
			continue
		}
		_, _ = s.Crawl(ctx, sc.Source) // the outcome is in the crawl panel
	}
	// New jobs (crawled, synced, added) get their distance a few at a time.
	_, _ = s.UpdateDistances(ctx, MaxLookupsPerPass)
}

// platformConnected reports whether id has a saved, not-expired session.
func (s *Service) platformConnected(ctx context.Context, id string) bool {
	ps, err := s.st.GetSession(ctx, id)
	return err == nil && ps.Status != "expired"
}

// IsBuiltinSource reports whether braveduck runs source itself.
func (s *Service) IsBuiltinSource(source string) bool {
	_, ok := s.builtinSources()[source]
	return ok
}
