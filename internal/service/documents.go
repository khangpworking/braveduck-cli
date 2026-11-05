package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// Tailored CVs and application emails, "bring your own AI" like rating:
// braveduck hands the user's agent (or any AI chat) a brief — profile, job,
// instructions — and stores what comes back. It never calls an LLM, holds no
// API key and sends no email itself: "send" opens the user's own mail app.

const cvInstructions = "Write a tailored CV for THIS job, in Markdown, using only facts from the " +
	"candidate's profile — never invent employers, dates, degrees or numbers. Lead with a short summary " +
	"aimed at the role, then put the most relevant experience and skills first and drop what doesn't help. " +
	"Use the job posting's language (if the posting is Vietnamese, write Vietnamese). Keep it to what fits " +
	"on 1-2 pages. Use # for the name, ## for sections, - for bullets, **bold** sparingly."

const draftInstructions = "Write a short application email for THIS job from the candidate. Use the " +
	"posting's language. 120-200 words: why this role, the two or three strongest matching proof points " +
	"from the profile, and a clear next step; mention the tailored CV is attached. No invented facts, no " +
	"clichés. Reply with a subject line and a plain-text body."

// DocBrief is what an agent needs to write a CV or an email for one job.
type DocBrief struct {
	Kind         string    `json:"kind"` // "cv" or "draft"
	Instructions string    `json:"instructions"`
	Profile      string    `json:"profile"`
	Job          RatingJob `json:"job"`
	ContactEmail string    `json:"contact_email,omitempty"`
	Notes        string    `json:"notes,omitempty"`    // the user's steering notes for a re-draft
	Previous     string    `json:"previous,omitempty"` // the current CV, for a re-draft
}

func (s *Service) docBrief(ctx context.Context, id int64, kind string) (DocBrief, jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return DocBrief{}, j, err
	}
	p, err := s.Profile(ctx)
	if err != nil {
		return DocBrief{}, j, err
	}
	if !p.Filled {
		return DocBrief{}, j, fmt.Errorf("%w — fill it in at %s first", ErrProfileEmpty, p.Path)
	}
	return DocBrief{Kind: kind, Profile: profileForRater(p.Text), Job: toRatingJob(j)}, j, nil
}

// CVBrief is the brief for tailoring the user's CV to job id.
func (s *Service) CVBrief(ctx context.Context, id int64) (DocBrief, error) {
	b, j, err := s.docBrief(ctx, id, "cv")
	if err != nil {
		return b, err
	}
	b.Instructions = cvInstructions
	if j.CVPath != "" {
		if prev, err := os.ReadFile(j.CVPath); err == nil {
			b.Previous = string(prev)
		}
	}
	return b, nil
}

// DraftBrief is the brief for an application email to job id.
func (s *Service) DraftBrief(ctx context.Context, id int64, notes string) (DocBrief, error) {
	b, j, err := s.docBrief(ctx, id, "draft")
	if err != nil {
		return b, err
	}
	b.Instructions = draftInstructions
	b.ContactEmail = j.ContactEmail
	b.Notes = strings.TrimSpace(notes)
	if b.Notes != "" {
		_ = s.st.SetDraftNotes(ctx, id, b.Notes, s.now())
	}
	return b, nil
}

const maxCVChars = 100_000

// maxCVVersions caps tailored CV versions per job (each is a file).
const maxCVVersions = 30

// SubmitCV stores a tailored CV (Markdown) for job id as a new version, in
// <data dir>/cvs, private to the user.
func (s *Service) SubmitCV(ctx context.Context, id int64, markdown string) (jobs.Job, error) {
	markdown = strings.TrimSpace(markdown)
	if len([]rune(markdown)) < 50 {
		return jobs.Job{}, fmt.Errorf("%w: the CV is empty or too short", ErrInvalid)
	}
	if len(markdown) > maxCVChars {
		return jobs.Job{}, fmt.Errorf("%w: the CV is over %d characters", ErrInvalid, maxCVChars)
	}
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	if s.profilePath == "" {
		return jobs.Job{}, errors.New("no data dir configured")
	}
	// CVs live next to the profile they're written from (the data dir).
	dir := filepath.Join(filepath.Dir(s.profilePath), "cvs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return jobs.Job{}, err
	}
	version := j.CVVersion + 1
	if version > maxCVVersions {
		return jobs.Job{}, fmt.Errorf("%w: this job already has %d CV versions", ErrInvalid, maxCVVersions)
	}
	path := filepath.Join(dir, fmt.Sprintf("job-%d-v%d.md", id, version))
	if err := os.WriteFile(path, []byte(markdown+"\n"), 0o600); err != nil {
		return jobs.Job{}, err
	}
	if err := s.st.SetCV(ctx, id, path, version, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	s.logEvent(ctx, "added", fmt.Sprintf("Tailored CV v%d for %q", version, j.Title))
	return s.GetJob(ctx, id)
}

// CV returns job id's current tailored CV (Markdown).
func (s *Service) CV(ctx context.Context, id int64) (string, jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return "", j, err
	}
	if j.CVPath == "" {
		return "", j, fmt.Errorf("%w: no tailored CV for this job yet", ErrNotFound)
	}
	b, err := os.ReadFile(j.CVPath)
	if err != nil {
		return "", j, fmt.Errorf("%w: the CV file is gone (%v)", ErrNotFound, err)
	}
	return string(b), j, nil
}

// Draft is an application email.
type Draft struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Draft statuses.
const (
	DraftReady    = "ready-to-review"
	DraftSent     = "sent"
	DraftRejected = "rejected"
)

// SubmitDraft stores an application email for job id, ready for review. An
// empty To falls back to the job's contact email.
func (s *Service) SubmitDraft(ctx context.Context, id int64, d Draft) (jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	d.To, d.Subject, d.Body = strings.TrimSpace(d.To), strings.TrimSpace(d.Subject), strings.TrimSpace(d.Body)
	if d.To == "" {
		d.To = j.ContactEmail
	}
	if d.To != "" {
		if _, err := mail.ParseAddress(d.To); err != nil {
			return jobs.Job{}, fmt.Errorf("%w: %q isn't an email address", ErrInvalid, d.To)
		}
	}
	if d.Subject == "" || len(d.Subject) > 300 {
		return jobs.Job{}, fmt.Errorf("%w: a subject (up to 300 characters) is required", ErrInvalid)
	}
	if len([]rune(d.Body)) < 20 || len(d.Body) > 20_000 {
		return jobs.Job{}, fmt.Errorf("%w: the body must be 20-20000 characters", ErrInvalid)
	}
	raw, _ := json.Marshal(d)
	if err := s.st.SetDraft(ctx, id, string(raw), DraftReady, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	s.logEvent(ctx, "added", fmt.Sprintf("Email draft ready for %q", j.Title))
	return s.GetJob(ctx, id)
}

// MarkDraftSent records that the user sent the draft (from their own mail
// app) and moves the job to Applied, starting the reply clock.
func (s *Service) MarkDraftSent(ctx context.Context, id int64) (jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return jobs.Job{}, err
	}
	if j.DraftEmail == "" {
		return jobs.Job{}, fmt.Errorf("%w: there's no draft to mark as sent", ErrInvalid)
	}
	if err := s.st.SetDraft(ctx, id, j.DraftEmail, DraftSent, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	if j.Status == "saved" || j.Status == "waiting" {
		_ = s.st.SetStatus(ctx, id, "applied", s.now())
	}
	_ = s.st.SetApplyState(ctx, id, "sent", "email", s.now())
	s.logEvent(ctx, "moved", fmt.Sprintf("Application email sent for %q", j.Title))
	return s.GetJob(ctx, id)
}

// RejectDraft discards job id's draft.
func (s *Service) RejectDraft(ctx context.Context, id int64) (jobs.Job, error) {
	if err := s.st.SetDraft(ctx, id, "", DraftRejected, s.now()); err != nil {
		return jobs.Job{}, notFound(err)
	}
	return s.GetJob(ctx, id)
}

// currentDraft decodes job id's draft, if it has one.
func (s *Service) currentDraft(ctx context.Context, id int64) (Draft, jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return Draft{}, j, err
	}
	var d Draft
	if j.DraftEmail == "" || json.Unmarshal([]byte(j.DraftEmail), &d) != nil {
		return Draft{}, j, fmt.Errorf("%w: no draft", ErrNotFound)
	}
	return d, j, nil
}
