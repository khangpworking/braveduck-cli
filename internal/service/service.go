// Package service is braveduck's one core: every operation the CLI, the REST
// API and the MCP server expose lives here exactly once, so the three faces
// can't drift apart (validation, duplicate checks, the activity feed). The
// faces only translate their input/output — flags, JSON, MCP tool calls — and
// map the errors below onto their own vocabulary (exit message, HTTP status,
// tool error).
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/apply"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Errors every face maps. Wrapped errors carry the detail; test with errors.Is.
var (
	ErrNotFound  = errors.New("no such job")
	ErrInvalid   = errors.New("invalid input")
	ErrDuplicate = store.ErrDuplicateURL
)

// Stages are the board's lanes, in order.
var Stages = []string{"saved", "waiting", "applied", "interview", "offer", "closed"}

// Service wraps the store with the business rules.
type Service struct {
	st  *store.Store
	now func() time.Time

	profilePath string         // see SetProfilePath / SetDataDir
	plat        *platformState // see SetDataDir

	// fetch extracts a JobPosting from a listing URL; swapped in tests so they
	// never touch the network.
	fetch func(ctx context.Context, url string) (scrape.JobPosting, bool, error)
}

// New returns a Service over st.
func New(st *store.Store) *Service {
	return &Service{st: st, now: time.Now, fetch: scrape.FetchJobPosting}
}

// --- jobs ---

// ListFilter narrows ListJobs; empty fields don't filter.
type ListFilter = store.ListFilter

// ListJobs returns matching jobs, newest first; never nil.
func (s *Service) ListJobs(ctx context.Context, f ListFilter) ([]jobs.Job, error) {
	list, err := s.st.List(ctx, f)
	if list == nil {
		list = []jobs.Job{}
	}
	return list, err
}

// GetJob returns one job or ErrNotFound.
func (s *Service) GetJob(ctx context.Context, id int64) (jobs.Job, error) {
	j, err := s.st.Get(ctx, id)
	return j, notFound(err)
}

// NewJob is a job described field by field (the CLI's `add`, MCP's add_job).
type NewJob struct {
	Title, Company, URL, Salary, Source, JD string
}

// AddJob stores a job described field by field.
func (s *Service) AddJob(ctx context.Context, in NewJob) (jobs.Job, error) {
	if strings.TrimSpace(in.Title) == "" {
		return jobs.Job{}, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	return s.add(ctx, jobs.Job{
		Title:           strings.TrimSpace(in.Title),
		Company:         in.Company,
		URL:             strings.TrimSpace(in.URL),
		Salary:          in.Salary,
		FullJD:          in.JD,
		DiscoverySource: in.Source,
	})
}

// AddFromContent is "Extract & Add", done without any AI: content is either a
// listing URL (fetched, and its schema.org JobPosting lifted verbatim) or a
// pasted JD (line/regex heuristics). Fields that can't be read stay empty.
func (s *Service) AddFromContent(ctx context.Context, content string) (jobs.Job, error) {
	content = strings.TrimSpace(content)
	if len(content) < 5 {
		return jobs.Job{}, fmt.Errorf("%w: paste a job URL or description", ErrInvalid)
	}

	var jp scrape.JobPosting
	j := jobs.Job{}
	if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
		j.URL = content
		// Check for a duplicate before the (slow) fetch; add re-checks atomically.
		dup, err := s.st.ExistsByURL(ctx, content)
		if err != nil {
			return jobs.Job{}, err
		}
		if dup {
			return jobs.Job{}, ErrDuplicate
		}
		// Best-effort: a blocked/JS-only page just yields a bare-URL card. A
		// link to localhost/LAN is refused outright — we never fetch those.
		jp, _, err = s.fetch(ctx, content)
		if errors.Is(err, scrape.ErrNonPublicURL) {
			return jobs.Job{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
	} else {
		jp = scrape.ParsePastedJD(content)
		j.DiscoverySource = "paste"
	}

	j.Title = jp.Title
	if j.Title == "" {
		j.Title = "Untitled listing"
	}
	j.Company = jp.Company
	j.Location = jp.Location
	j.Salary = jp.Salary
	j.Skills = strings.Join(jp.Skills, ", ")
	j.FullJD = jp.Description
	j.ContactEmail = jp.Email
	if jp.Deadline != "" {
		j.ExpiresAt = jobs.ExpirePrefix + jp.Deadline
	}
	return s.add(ctx, j)
}

// add applies the rules every new job gets, stores it, and logs it.
func (s *Service) add(ctx context.Context, j jobs.Job) (jobs.Job, error) {
	j.Status = "saved"
	j.ScoreStatus = "pending" // queued for a rater
	if j.DiscoverySource == "" && j.URL != "" {
		j.DiscoverySource = scrape.SourceFor(j.URL)
	}
	// Read the contact email / email-vs-platform method straight off the JD.
	if j.FullJD != "" {
		cls := apply.ClassifyApply(j.FullJD)
		j.ApplyMethod = string(cls.Method)
		if j.ContactEmail == "" {
			j.ContactEmail = cls.ContactEmail
		}
	}

	id, err := s.st.Add(ctx, j, s.now())
	if err != nil {
		return jobs.Job{}, err
	}
	saved, err := s.st.Get(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	s.logEvent(ctx, "added", fmt.Sprintf("Added %q", saved.Title))
	return saved, nil
}

// Patch names the board-editable fields to change; nil leaves a field as is.
type Patch = store.JobPatch

// UpdateJob applies p and returns the updated job.
func (s *Service) UpdateJob(ctx context.Context, id int64, p Patch) (jobs.Job, error) {
	if p.Status != nil {
		st := strings.ToLower(strings.TrimSpace(*p.Status))
		if !validStage(st) {
			return jobs.Job{}, fmt.Errorf("%w: unknown stage %q (%s)", ErrInvalid, *p.Status, strings.Join(Stages, "/"))
		}
		p.Status = &st
	}
	before, err := s.GetJob(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	if err := s.st.Patch(ctx, id, p, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	if p.Status != nil && *p.Status != before.Status {
		s.logEvent(ctx, "moved", fmt.Sprintf("Moved %q to %s", before.Title, *p.Status))
	}
	return s.GetJob(ctx, id)
}

// SetStatus moves a job to another lane.
func (s *Service) SetStatus(ctx context.Context, id int64, stage string) (jobs.Job, error) {
	return s.UpdateJob(ctx, id, Patch{Status: &stage})
}

// TogglePin flips pinned and returns the updated job.
func (s *Service) TogglePin(ctx context.Context, id int64) (jobs.Job, error) {
	return s.toggle(ctx, id, "pinned")
}

// ToggleKeep flips keep (exempt from auto-clear) and returns the updated job.
func (s *Service) ToggleKeep(ctx context.Context, id int64) (jobs.Job, error) {
	return s.toggle(ctx, id, "keep")
}

func (s *Service) toggle(ctx context.Context, id int64, flag string) (jobs.Job, error) {
	if err := s.st.Toggle(ctx, id, flag, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	return s.GetJob(ctx, id)
}

// DeleteJob removes a job (and its ratings) and returns what was deleted.
func (s *Service) DeleteJob(ctx context.Context, id int64) (jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	if err := s.st.Delete(ctx, id); err != nil {
		return jobs.Job{}, notFound(err)
	}
	s.logEvent(ctx, "removed", fmt.Sprintf("Removed %q", j.Title))
	return j, nil
}

// --- config, keywords, status feeds ---

// Stats is the board's funnel: jobs per lane plus the interview-rate
// ("morale") figures, computed by the ported jobs.SuccessRateOf logic.
type Stats struct {
	Total         int            `json:"total"`
	ByStage       map[string]int `json:"by_stage"`
	Resolved      int            `json:"resolved"`
	Interviews    int            `json:"interviews"`
	Pending       int            `json:"pending"`
	InterviewRate float64        `json:"interview_rate"` // interviews / resolved; 0 until something resolves
	Morale        string         `json:"morale"`
}

// Stats computes the funnel over every job.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	all, err := s.st.List(ctx, store.ListFilter{})
	if err != nil {
		return Stats{}, err
	}
	st := Stats{Total: len(all), ByStage: map[string]int{}}
	for _, stage := range Stages {
		st.ByStage[stage] = 0
	}
	for _, j := range all {
		st.ByStage[j.Status]++
	}
	sr := jobs.SuccessRateOf(all, s.now())
	st.Resolved, st.Interviews, st.Pending, st.InterviewRate = sr.Resolved, sr.Interviews, sr.Pending, sr.Rate
	st.Morale = jobs.MoraleMessage(sr).Line
	return st, nil
}

// RatingConfig returns the A/B rater config (normalized, never blocked).
func (s *Service) RatingConfig(ctx context.Context) (rating.Config, error) {
	return s.st.GetRatingConfig(ctx)
}

// SetRatingConfig validates strictly and saves.
func (s *Service) SetRatingConfig(ctx context.Context, c rating.Config) (rating.Config, error) {
	if err := c.Validate(); err != nil {
		return rating.Config{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return s.st.SetRatingConfig(ctx, c)
}

// Keywords returns every crawler keyword list.
func (s *Service) Keywords(ctx context.Context) (keyword.Sets, error) {
	return s.st.AllKeywords(ctx)
}

// CrawlStatus reports per-source crawl health and today's find count.
func (s *Service) CrawlStatus(ctx context.Context) (store.CrawlStatusView, error) {
	return s.st.CrawlStatus(ctx, s.now())
}

// crawlSourceRE is what a source name may look like: the board shows it and
// jobs carry it as discovery_source, so keep it a plain id.
var crawlSourceRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ReportCrawl records one run of an external crawler (fb-watcher, …) so the
// board's crawl panel shows it and flags it when it fails or goes quiet. A
// failure that follows a success is also put in the activity feed, once.
func (s *Service) ReportCrawl(ctx context.Context, source string, ok bool, reason string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	if !crawlSourceRE.MatchString(source) {
		return fmt.Errorf("%w: source %q: use lowercase letters, digits, - or _", ErrInvalid, source)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 300 {
		reason = reason[:300]
	}
	wasFailing := false
	if v, err := s.st.CrawlStatus(ctx, s.now()); err == nil {
		for _, src := range v.Sources {
			if src.Name == source && src.LastError != "" {
				wasFailing = true
			}
		}
	}
	if err := s.st.RecordCrawlRun(ctx, source, ok, reason, s.now()); err != nil {
		return err
	}
	if !ok && !wasFailing {
		msg := fmt.Sprintf("%s crawl failed", source)
		if reason != "" {
			msg += ": " + reason
		}
		s.logEvent(ctx, "error", msg)
	}
	return nil
}

// Activity returns the newest activity-feed events.
func (s *Service) Activity(ctx context.Context, limit int) ([]store.Event, error) {
	return s.st.RecentEvents(ctx, limit)
}

// --- helpers ---

func validStage(s string) bool {
	for _, st := range Stages {
		if s == st {
			return true
		}
	}
	return false
}

// notFound maps the store's not-found to ErrNotFound; other errors pass.
func notFound(err error) error {
	if store.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}

// logEvent records an activity-feed entry best-effort: a logging failure must
// never fail the mutation that triggered it. A cancelled context is ignored
// too, so a client hanging up mid-request doesn't lose the log line.
func (s *Service) logEvent(ctx context.Context, typ, message string) {
	_ = s.st.LogEvent(context.WithoutCancel(ctx), typ, message, s.now())
}
