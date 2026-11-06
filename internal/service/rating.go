package service

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// Rating is "bring your own agent": braveduck never calls an LLM or holds an
// API key. It hands the connected agent (or any AI chat, via `braveduck rate`)
// a brief — the user's profile, the rubric and the job — and validates and
// stores the verdict that comes back.

//go:embed profile_template.md
var profileTemplate string

// ErrProfileEmpty means the profile is still the blank template, so a score
// would mean nothing. The message names the file to edit.
var ErrProfileEmpty = errors.New("your profile is empty")

// SetProfilePath tells the service where the user's profile lives (normally
// profile.md next to the database). Profile features fail until it is set.
func (s *Service) SetProfilePath(p string) { s.profilePath = p }

// Profile is the user's profile file.
type Profile struct {
	Path   string `json:"path"`
	Text   string `json:"text"`
	Filled bool   `json:"filled"` // false while it's still the blank template
}

// Profile returns the profile, creating it from the template on first use
// (private to the user: 0600). An existing file is never overwritten.
func (s *Service) Profile(_ context.Context) (Profile, error) {
	if s.profilePath == "" {
		return Profile{}, errors.New("no profile path configured")
	}
	b, err := os.ReadFile(s.profilePath)
	if errors.Is(err, os.ErrNotExist) {
		if err := createProfile(s.profilePath); err != nil {
			return Profile{}, err
		}
		b, err = os.ReadFile(s.profilePath)
	}
	if err != nil {
		return Profile{}, err
	}
	text := string(b)
	return Profile{Path: s.profilePath, Text: text, Filled: profileFilled(text)}, nil
}

// createProfile writes the template to path, user-only. O_EXCL means a file
// that appeared meanwhile (another braveduck process) is kept, not clobbered.
func createProfile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = f.WriteString(profileTemplate)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

var (
	htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)
	// A template line with nothing filled in: a heading, a bare bullet, or a
	// "Label:" with no value after the colon.
	emptyLineRE = regexp.MustCompile(`^\s*(#.*|[-*]\s*|[-*]?\s*[^:]{1,80}:\s*)$`)
)

// profileFilled reports whether the user has written anything real: some line
// survives once comments, headings, bare bullets and empty "Label:" lines are
// dropped.
func profileFilled(text string) bool {
	for _, line := range strings.Split(htmlCommentRE.ReplaceAllString(text, ""), "\n") {
		if strings.TrimSpace(line) != "" && !emptyLineRE.MatchString(line) {
			return true
		}
	}
	return false
}

// profileForRater is the profile with the template's scaffolding removed —
// comments, unfilled "Label:" lines, bare bullets and headings left with
// nothing under them — so the rater reads only what the user wrote.
func profileForRater(text string) string {
	var out []string
	pendingHeading := ""
	for _, line := range strings.Split(htmlCommentRE.ReplaceAllString(text, ""), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "#"):
			pendingHeading = t
		case t == "" || emptyLineRE.MatchString(line):
			// scaffolding or spacing
		default:
			if pendingHeading != "" {
				if len(out) > 0 {
					out = append(out, "")
				}
				out = append(out, pendingHeading)
				pendingHeading = ""
			}
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// RatingJob is the part of a job a rater needs.
type RatingJob struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location,omitempty"`
	Salary      string `json:"salary,omitempty"`
	Skills      string `json:"skills,omitempty"`
	URL         string `json:"url,omitempty"`
	Source      string `json:"source,omitempty"`
	Description string `json:"description,omitempty"`
}

// RatingBrief is everything a rater needs: who the candidate is, how to score,
// which model id to report as, and the jobs to score.
type RatingBrief struct {
	Profile      string      `json:"profile"`
	ProfilePath  string      `json:"profile_path"`
	Rubric       string      `json:"rubric"`
	DefaultModel string      `json:"default_model"`   // its verdicts become the board badge
	Model        string      `json:"model,omitempty"` // selected rater for an on-demand request
	Jobs         []RatingJob `json:"jobs"`
}

// maxJD caps the description per job so a big queue stays a sane prompt.
const maxJD = 6000

func toRatingJob(j jobs.Job) RatingJob {
	jd := j.FullJD
	if jd == "" {
		jd = j.Summary
	}
	if len(jd) > maxJD {
		jd = jd[:maxJD] + "…"
	}
	return RatingJob{
		ID: j.ID, Title: j.Title, Company: j.Company, Location: j.Location,
		Salary: j.Salary, Skills: j.Skills, URL: j.URL, Source: j.DiscoverySource,
		Description: jd,
	}
}

// brief assembles a RatingBrief, refusing while the profile is empty.
func (s *Service) brief(ctx context.Context, js []jobs.Job) (RatingBrief, error) {
	p, err := s.Profile(ctx)
	if err != nil {
		return RatingBrief{}, err
	}
	if !p.Filled {
		return RatingBrief{}, fmt.Errorf("%w — fill it in at %s, then rate again", ErrProfileEmpty, p.Path)
	}
	cfg, err := s.raterConfig(ctx)
	if err != nil {
		return RatingBrief{}, err
	}
	b := RatingBrief{
		Profile: profileForRater(p.Text), ProfilePath: p.Path, Rubric: rating.DefaultRubric,
		DefaultModel: cfg.DefaultModel, Jobs: []RatingJob{},
	}
	for _, j := range js {
		b.Jobs = append(b.Jobs, toRatingJob(j))
	}
	return b, nil
}

// RatingQueue returns a brief for the jobs still waiting for a score (oldest
// first, so nothing starves), skipping closed jobs. limit <= 0 means 10.
func (s *Service) RatingQueue(ctx context.Context, limit int) (RatingBrief, error) {
	if limit <= 0 {
		limit = 10
	}
	all, err := s.st.List(ctx, ListFilter{})
	if err != nil {
		return RatingBrief{}, err
	}
	var queue []jobs.Job
	for i := len(all) - 1; i >= 0 && len(queue) < limit; i-- { // List is newest-first
		if j := all[i]; j.ScoreStatus != "scored" && j.Status != "closed" {
			queue = append(queue, j)
		}
	}
	return s.brief(ctx, queue)
}

// RateBrief returns a brief for one job, scored or not (re-rating is allowed).
func (s *Service) RateBrief(ctx context.Context, id int64) (RatingBrief, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return RatingBrief{}, err
	}
	return s.brief(ctx, []jobs.Job{j})
}

// RatingRequest either contains a prompt for the selected external rater or
// the result of an immediate Jev rating. Model names the verdict's owner.
type RatingRequest struct {
	Prompt string    `json:"prompt,omitempty"`
	Brief  string    `json:"brief,omitempty"`
	Model  string    `json:"model"`
	Rated  bool      `json:"rated,omitempty"`
	Job    *jobs.Job `json:"job,omitempty"`
}

// RequestRating honors an explicit model; an empty choice uses the default.
// Choosing a challenger must never silently invoke the default Jev rater.
func (s *Service) RequestRating(ctx context.Context, id int64, model string) (RatingRequest, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		// A Jev key lets a normal user rate without an agent, unless they
		// already picked a different rater.
		s.autoEnableJev(ctx)
		cfg, err := s.raterConfig(ctx)
		if err != nil {
			return RatingRequest{}, err
		}
		model = cfg.DefaultModel
	}
	if IsJevModel(model) {
		j, err := s.JevRate(ctx, id)
		if err != nil {
			return RatingRequest{}, err
		}
		// JevRate records the actual supported Jev model, including when it
		// is a challenger and leaves the top-level board badge unchanged.
		return RatingRequest{Model: jev.DefaultModel, Rated: true, Job: &j}, nil
	}
	b, err := s.RateBrief(ctx, id)
	if err != nil {
		return RatingRequest{}, err
	}
	b.Model = model
	prompt := fmt.Sprintf("%s Submit the verdict with model %q.", AgentPrompt("rate", id, b.Jobs[0].Title), model)
	return RatingRequest{Model: model, Prompt: prompt, Brief: RenderRatingBrief(b)}, nil
}

// Verdict is a rater's answer for one job.
type Verdict struct {
	JobID     int64
	Model     string // the rater's id; empty means the default rater
	Score     float64
	Letter    string // optional: derived from Score when empty
	Reasoning string
}

// SubmitRating validates a verdict (rating.ValidateVerdict) and stores it.
// The default rater's verdict becomes the board badge; another model's is kept
// as an A/B challenger rating.
func (s *Service) SubmitRating(ctx context.Context, v Verdict) (jobs.Job, error) {
	letter := v.Letter
	if strings.TrimSpace(letter) == "" {
		letter = rating.LetterForScore(v.Score)
	}
	ok, err := rating.ValidateVerdict(rating.RawVerdict{
		JobID: v.JobID, Score: v.Score, Letter: letter, Reasoning: v.Reasoning,
	})
	if err != nil {
		return jobs.Job{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	cfg, err := s.raterConfig(ctx)
	if err != nil {
		return jobs.Job{}, err
	}
	model := strings.TrimSpace(v.Model)
	owns := rating.OwnsBoardBadge(model, cfg.DefaultModel, cfg.ABEnabled)
	if model == "" {
		model = cfg.DefaultModel
	}
	if err := s.st.SaveRating(ctx, model, ok, owns, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	j, err := s.GetJob(ctx, v.JobID)
	if err != nil {
		return jobs.Job{}, err
	}
	s.logEvent(ctx, "rated", fmt.Sprintf("Rated %q %s %.1f (%s)", j.Title, ok.Letter, ok.Score, model))
	return j, nil
}
