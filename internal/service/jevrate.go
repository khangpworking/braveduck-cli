package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// Jev rating: Jev (TypeSafe AI's decision model) answers multiple-choice
// questions about "this candidate × this job". The score is the
// probability-weighted average of the letter bands, the letter follows from
// the score, and the reasons are built from the sub-answers. The user chose
// to send their full profile; see docs/specs/jev-rating.md.

// JevRateSource is the schedule/crawl-panel id of automatic Jev rating.
const JevRateSource = "jev-rate"

const defaultJevEveryMinutes = 30

// jevAsker is the Jev API; swapped in tests.
type jevAsker interface {
	Ask(ctx context.Context, state string, questions map[string]jev.Question) (map[string]jev.Answer, error)
}

// SetJev replaces the Jev client (tests).
func (s *Service) SetJev(a jevAsker) { s.jev = a }

// ErrJevLimit means today's Jev ratings are used up (a shared server's
// budget per user); more are possible tomorrow.
var ErrJevLimit = fmt.Errorf("%w: today's Jev ratings are used up on this server — more tomorrow, or rate with your own AI agent", ErrInvalid)

// SetJevDailyLimit caps Jev calls per day for this service (on a shared
// server the admin's key pays for every user); 0 means no cap.
func (s *Service) SetJevDailyLimit(n int) { s.jevDaily = n }

// JevDailyLimit is the cap SetJevDailyLimit set; 0 means none.
func (s *Service) JevDailyLimit() int { return s.jevDaily }

// jevCallsKey is the settings key of today's Jev call count ("date count").
const jevCallsKey = "jev_calls"

// takeJevCall counts one Jev call against today's limit, or returns
// ErrJevLimit when none are left.
func (s *Service) takeJevCall(ctx context.Context) error {
	if s.jevDaily <= 0 {
		return nil
	}
	s.jevMu.Lock()
	defer s.jevMu.Unlock()
	today := s.now().Format("2006-01-02")
	used := 0
	if v, ok, err := s.st.Setting(ctx, jevCallsKey); err != nil {
		return err
	} else if date, n, found := strings.Cut(v, " "); ok && found && date == today {
		used, _ = strconv.Atoi(n)
	}
	if used >= s.jevDaily {
		return ErrJevLimit
	}
	return s.st.SetSetting(ctx, jevCallsKey, fmt.Sprintf("%s %d", today, used+1))
}

// loadJevKey finds the API key; tests replace it so they can never reach the
// real API with the user's key.
var loadJevKey = jev.LoadKey

func (s *Service) jevClient() (jevAsker, error) {
	if s.jev != nil {
		return s.jev, nil
	}
	key, _, err := loadJevKey()
	if err != nil {
		return nil, err
	}
	return jev.New(key), nil
}

// IsJevModel reports whether a rater id is a Jev model.
func IsJevModel(id string) bool { return strings.HasPrefix(id, "jev-") }

// letterValue is each band's midpoint on the 1.0-5.0 scale (rating rubric).
var letterValue = map[string]float64{"A": 4.75, "B": 4.2, "C": 3.7, "D": 3.2, "F": 2.0}

var jevQuestions = map[string]jev.Question{
	"fit": {Type: "choice",
		Instructions: "Overall, how good is this job for THIS candidate (their profile is in the text)? Weigh skills match, target-role alignment, compensation vs their expectations, culture signals, and red flags. Score low when core required skills are missing, even if the title sounds appealing.",
		Criteria: map[string]string{
			"A": "Strong fit — apply now: core skills match, on target, no deal-breakers.",
			"B": "Good fit: most requirements match, minor gaps.",
			"C": "Decent: some match, notable gaps or uncertainty.",
			"D": "Weak: significant gaps or misaligned with what they want.",
			"F": "Skip: wrong role, missing core skills, or a deal-breaker.",
		}},
	"skills": {Type: "choice", Instructions: "How well do the candidate's skills and experience match the job's requirements?",
		Criteria: map[string]string{"strong": "Most core requirements are clearly met.", "partial": "Some core requirements are met; others are missing.", "weak": "Few or none of the core requirements are met.", "unclear": "The posting doesn't say enough to tell."}},
	"role": {Type: "choice", Instructions: "Is this the kind of role the candidate says they want?",
		Criteria: map[string]string{"on_target": "Matches their target roles.", "adjacent": "Related but not what they asked for.", "off_target": "Not what they want.", "unclear": "Can't tell from the text."}},
	"seniority": {Type: "choice", Instructions: "Does the job's seniority match the candidate's level?",
		Criteria: map[string]string{"match": "About right.", "above": "Asks for more experience than they have.", "below": "Below their level.", "unclear": "Not stated."}},
	"pay": {Type: "choice", Instructions: "Does the stated pay meet the candidate's minimum?",
		Criteria: map[string]string{"meets": "At or above their minimum.", "below": "Below their minimum.", "unclear": "Pay or their minimum isn't stated (e.g. 'negotiable')."}},
	"dealbreaker": {Type: "choice", Instructions: "Does the job hit any of the candidate's stated deal-breakers?",
		Criteria: map[string]string{"none": "No deal-breaker applies.", "some": "At least one deal-breaker applies."}},
}

var jevLabels = map[string]map[string]string{
	"skills":      {"strong": "skills: strong match", "partial": "skills: partial match", "weak": "skills: weak match"},
	"role":        {"on_target": "role: on target", "adjacent": "role: adjacent", "off_target": "role: off target"},
	"seniority":   {"match": "seniority: matches", "above": "seniority: above your level", "below": "seniority: below your level"},
	"pay":         {"meets": "pay: meets your minimum", "below": "pay: below your minimum"},
	"dealbreaker": {"some": "⚠ hits a deal-breaker"},
}

// jevState is what Jev reads: the rubric, the full profile and the job.
func jevState(b RatingBrief) string {
	j := b.Jobs[0]
	var sb strings.Builder
	sb.WriteString("CANDIDATE PROFILE\n" + strings.TrimSpace(b.Profile) + "\n\nJOB\n")
	fmt.Fprintf(&sb, "Title: %s\nCompany: %s\n", j.Title, j.Company)
	for _, kv := range [][2]string{{"Location", j.Location}, {"Salary", j.Salary}, {"Skills", j.Skills}} {
		if kv[1] != "" {
			fmt.Fprintf(&sb, "%s: %s\n", kv[0], kv[1])
		}
	}
	if j.Description != "" {
		sb.WriteString("\n" + strings.TrimSpace(j.Description) + "\n")
	}
	return sb.String()
}

// JevVerdict turns Jev's answers into a verdict: expected score, letter from
// the score, and reasons from the sub-answers.
func JevVerdict(id int64, ans map[string]jev.Answer) Verdict {
	score := 0.0
	for letter, p := range ans["fit"].Probabilities {
		score += p * letterValue[letter]
	}
	score = math.Round(score*10) / 10
	reasons := []string{fmt.Sprintf("Jev: %s (%.0f%% sure)", ans["fit"].Choice, 100*ans["fit"].Confidence())}
	for _, q := range []string{"skills", "role", "seniority", "pay", "dealbreaker"} {
		if l, ok := jevLabels[q][ans[q].Choice]; ok {
			reasons = append(reasons, l)
		}
	}
	return Verdict{JobID: id, Model: jev.DefaultModel, Score: score, Letter: rating.LetterForScore(score), Reasoning: strings.Join(reasons, " • ")}
}

// JevRate rates job id with Jev now and stores the verdict (the badge, when
// Jev is the default rater).
func (s *Service) JevRate(ctx context.Context, id int64) (jobs.Job, error) {
	if out, err := s.JevOptedOut(ctx); err != nil {
		return jobs.Job{}, err
	} else if out {
		return jobs.Job{}, ErrJevOptedOut
	}
	b, err := s.RateBrief(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	c, err := s.jevClient()
	if err != nil {
		return jobs.Job{}, err
	}
	if err := s.takeJevCall(ctx); err != nil {
		return jobs.Job{}, err
	}
	ans, err := c.Ask(ctx, jevState(b), jevQuestions)
	if err != nil {
		return jobs.Job{}, err
	}
	return s.SubmitRating(ctx, JevVerdict(id, ans))
}

// JevRateQueue rates up to limit unrated jobs with Jev (oldest first). It
// stops at the first API error so a bad key or outage isn't hammered.
func (s *Service) JevRateQueue(ctx context.Context, limit int) (rated int, err error) {
	b, err := s.RatingQueue(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, j := range b.Jobs {
		if _, err := s.JevRate(ctx, j.ID); err != nil {
			return rated, err
		}
		rated++
	}
	return rated, nil
}

// jevIsDefault reports whether the board badge belongs to a Jev model.
func (s *Service) jevIsDefault(ctx context.Context) bool {
	c, err := s.raterConfig(ctx)
	return err == nil && IsJevModel(c.DefaultModel)
}

// jevOptOutKey is the settings key set when the user chose not to send their
// profile to Jev. Settings live in each user's own database, so on a shared
// server one user's choice never affects another.
const jevOptOutKey = "jev_opt_out"

// ErrJevOptedOut means the user turned Jev off for their profile.
var ErrJevOptedOut = fmt.Errorf("%w: you chose not to send your profile to Jev — turn it back on in Settings → Rating", ErrInvalid)

// JevOptedOut reports whether the user chose not to send their profile to Jev.
func (s *Service) JevOptedOut(ctx context.Context) (bool, error) {
	v, _, err := s.st.Setting(ctx, jevOptOutKey)
	return v == "1", err
}

// SetJevOptOut records whether the user's profile may be sent to Jev.
func (s *Service) SetJevOptOut(ctx context.Context, out bool) error {
	v := ""
	if out {
		v = "1"
	}
	return s.st.SetSetting(ctx, jevOptOutKey, v)
}

// raterConfig is the rater config this user's ratings follow. Once they opted
// out of Jev, Jev isn't a rater for them: the raters it kept as challengers
// (or the out-of-box rater) take over, so Rate asks their own AI instead.
func (s *Service) raterConfig(ctx context.Context) (rating.Config, error) {
	cfg, err := s.RatingConfig(ctx)
	if err != nil || !IsJevModel(cfg.DefaultModel) {
		return cfg, err
	}
	if out, err := s.JevOptedOut(ctx); err != nil || !out {
		return cfg, err
	}
	var models []string
	for _, m := range cfg.Models {
		if !IsJevModel(m) {
			models = append(models, m)
		}
	}
	return rating.Normalize(rating.Config{ABEnabled: cfg.ABEnabled && len(models) > 1, Models: models}), nil
}

// JevAvailable reports whether a Jev key (or client) is there to rate with.
func (s *Service) JevAvailable() bool {
	_, err := s.jevClient()
	return err == nil
}

// autoEnableJev makes Jev the rater on a board that has a Jev key and where the
// owner has never picked a rater. A board the user configured is left alone,
// and nothing runs once the user opted out of Jev. It reports whether Jev
// should rate automatically.
func (s *Service) autoEnableJev(ctx context.Context) bool {
	if out, err := s.JevOptedOut(ctx); err != nil || out {
		return false
	}
	if s.jevIsDefault(ctx) {
		return true
	}
	chosen, err := s.st.RatingConfigChosen(ctx)
	if err != nil || chosen {
		return false
	}
	if s.jev == nil {
		if _, _, err := loadJevKey(); err != nil {
			return false
		}
	}
	_, err = s.EnableJev(ctx)
	return err == nil
}

// runJevRate is the scheduled task: rate new jobs when Jev owns the badge, a
// key exists and the profile is filled in; otherwise it quietly does nothing.
func (s *Service) runJevRate(ctx context.Context) (CrawlResult, error) {
	res := CrawlResult{Source: JevRateSource}
	if !s.autoEnableJev(ctx) {
		return res, nil
	}
	if _, err := s.jevClient(); err != nil {
		return res, err
	}
	n, err := s.JevRateQueue(ctx, 20)
	res.Found, res.Added = n, n
	if errors.Is(err, ErrProfileEmpty) || errors.Is(err, ErrJevLimit) {
		return res, nil // nothing to judge against yet, or done for today; not a failure of Jev
	}
	return res, err
}

// EnableJev makes Jev the default rater (the badge) and keeps the current
// raters as A/B challengers, up to the 3-model cap.
func (s *Service) EnableJev(ctx context.Context) (rating.Config, error) {
	if _, err := s.jevClient(); err != nil {
		return rating.Config{}, err
	}
	cur, err := s.RatingConfig(ctx)
	if err != nil {
		return rating.Config{}, err
	}
	models := []string{jev.DefaultModel}
	for _, m := range cur.Models {
		if m != jev.DefaultModel && len(models) < rating.MaxModels {
			models = append(models, m)
		}
	}
	return s.SetRatingConfig(ctx, rating.Config{ABEnabled: len(models) > 1, Models: models, DefaultModel: jev.DefaultModel})
}

// JevKeySource says where the Jev key would come from ("" when none), for doctor.
func JevKeySource() string {
	_, src, err := loadJevKey()
	if err != nil {
		return ""
	}
	return src
}
