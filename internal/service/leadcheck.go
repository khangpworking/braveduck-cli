package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// Jev lead check: sources like Facebook groups mix real job offers with
// courses, guides, tool promos, events and people looking for work. Jev
// classifies each new post when it's added; one it's confident isn't a job
// goes straight to Closed with the reason, so nothing is ever lost and a
// mistake is one drag away from being undone. Any Jev failure means "keep":
// an outage can never lose a lead.

// LeadCheckThreshold is how sure Jev must be that a post isn't a job.
const LeadCheckThreshold = 0.75

const leadCheckKey = "lead_check_sources"

// defaultLeadCheckSources are checked unless the user changes the setting.
var defaultLeadCheckSources = []string{"facebook"}

var leadQuestion = map[string]jev.Question{
	"kind": {Type: "choice",
		Instructions: "This text was posted in a job-seeking community. Is it an offer of work that the reader could apply for?",
		Criteria: map[string]string{
			"job_offer":     "An employer or recruiter is hiring for a specific role (full-time, part-time, internship).",
			"freelance_gig": "Someone offers a paid project, contract or freelance task to be done.",
			"not_job":       "Not an offer of work: a course, guide, tool or product promo, event, news, a question or discussion, or someone looking for a job.",
			"unclear":       "Too short or ambiguous to tell.",
		}},
}

// LeadCheckSources returns the sources whose new posts are checked.
func (s *Service) LeadCheckSources(ctx context.Context) []string {
	v, ok, err := s.st.Setting(ctx, leadCheckKey)
	if err != nil || !ok {
		return append([]string(nil), defaultLeadCheckSources...)
	}
	var out []string
	for _, src := range strings.Split(v, ",") {
		if src = strings.TrimSpace(src); src != "" {
			out = append(out, src)
		}
	}
	return out
}

// SetLeadCheck turns the lead check on or off for a source.
func (s *Service) SetLeadCheck(ctx context.Context, source string, on bool) ([]string, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if !crawlSourceRE.MatchString(source) {
		return nil, fmt.Errorf("%w: source %q: use lowercase letters, digits, - or _", ErrInvalid, source)
	}
	set := map[string]bool{}
	for _, s := range s.LeadCheckSources(ctx) {
		set[s] = true
	}
	set[source] = on
	var out []string
	for src, keep := range set {
		if keep {
			out = append(out, src)
		}
	}
	sort.Strings(out)
	return out, s.st.SetSetting(ctx, leadCheckKey, strings.Join(out, ","))
}

// LeadVerdict is Jev's call on one post.
type LeadVerdict struct {
	Kind       string  `json:"kind"` // job_offer / freelance_gig / not_job / unclear
	Confidence float64 `json:"confidence"`
	NotJob     bool    `json:"not_job"` // not_job at or above the threshold
}

// classifyLead asks Jev what a post is.
func (s *Service) classifyLead(ctx context.Context, j jobs.Job) (LeadVerdict, error) {
	c, err := s.jevClient()
	if err != nil {
		return LeadVerdict{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	text := strings.TrimSpace(j.Title + "\n\n" + j.FullJD)
	if len(text) > maxJD {
		text = text[:maxJD]
	}
	ans, err := c.Ask(ctx, text, leadQuestion)
	if err != nil {
		return LeadVerdict{}, err
	}
	k := ans["kind"]
	return LeadVerdict{Kind: k.Choice, Confidence: k.Confidence(),
		NotJob: k.Choice == "not_job" && k.Confidence() >= LeadCheckThreshold}, nil
}

// leadCheck runs on add: for a checked source, returns a note and true when
// Jev is confident the post isn't a job. Errors (no key, outage) mean keep.
func (s *Service) leadCheck(ctx context.Context, j jobs.Job) (string, bool) {
	checked := false
	for _, src := range s.LeadCheckSources(ctx) {
		checked = checked || src == j.DiscoverySource
	}
	if !checked {
		return "", false
	}
	v, err := s.classifyLead(ctx, j)
	if err != nil || !v.NotJob {
		return "", false
	}
	return fmt.Sprintf("Jev: not a job offer (%.0f%% sure) — closed automatically; drag it back if Jev got it wrong.", 100*v.Confidence), true
}

// LeadResult is one job's lead-check outcome.
type LeadResult struct {
	JobID int64       `json:"job_id"`
	Title string      `json:"title"`
	Lead  LeadVerdict `json:"verdict"`
}

// LeadPreview classifies the Saved jobs from checked sources without
// changing anything.
func (s *Service) LeadPreview(ctx context.Context) ([]LeadResult, error) {
	var out []LeadResult
	for _, src := range s.LeadCheckSources(ctx) {
		list, err := s.st.List(ctx, ListFilter{Source: src, Status: "saved"})
		if err != nil {
			return out, err
		}
		for _, j := range list {
			v, err := s.classifyLead(ctx, j)
			if err != nil {
				return out, err
			}
			out = append(out, LeadResult{JobID: j.ID, Title: j.Title, Lead: v})
		}
	}
	return out, nil
}

// LeadRecheck runs the check over jobs already on the board from checked
// sources that are still Saved, closing the ones Jev is sure aren't jobs.
func (s *Service) LeadRecheck(ctx context.Context) (checked, closed int, err error) {
	for _, src := range s.LeadCheckSources(ctx) {
		list, err := s.st.List(ctx, ListFilter{Source: src, Status: "saved"})
		if err != nil {
			return checked, closed, err
		}
		for _, j := range list {
			v, err := s.classifyLead(ctx, j)
			if err != nil {
				return checked, closed, err
			}
			checked++
			if !v.NotJob {
				continue
			}
			note := fmt.Sprintf("Jev: not a job offer (%.0f%% sure) — closed by lead re-check.", 100*v.Confidence)
			notes := strings.TrimSpace(note + "\n" + j.Notes)
			if _, err := s.UpdateJob(ctx, j.ID, Patch{Status: strPtr("closed"), Notes: &notes}); err == nil {
				closed++
			}
		}
	}
	return checked, closed, nil
}
