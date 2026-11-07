package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/apply"
	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/markdown"
	"github.com/khangpworking/braveduck-cli/internal/core/platform"
)

// Assisted apply: the user approves each application by starting it, and
// submits it themselves. braveduck prepares the CV, opens a visible window on
// the job with their login on that site (any platform they're logged in to),
// and afterwards confirms: from TopCV's own application history on TopCV, by
// asking the user anywhere else. It never clicks a submit button and never
// fills the form: the sites' submit calls are undocumented, so automating
// them could send a wrong application under the user's name.

// DailyApplyCap limits assisted applications per day, to protect the account.
const DailyApplyCap = 10

// ErrApplyLimit means the daily cap or the cooldown between applies is in effect.
var ErrApplyLimit = errors.New("apply limit")

// ApplyPrep is everything the user needs before the window opens.
type ApplyPrep struct {
	JobID       int64  `json:"job_id"`
	URL         string `json:"url"`
	Site        string `json:"site"`         // the job site's name, e.g. TopCV
	SiteID      string `json:"site_id"`      // its platform id, e.g. topcv
	CVPDF       string `json:"cv_pdf"`       // the CV to pick in TopCV's upload dialog: the tailored one as PDF, else the base CV (PDF or .docx)
	CoverLetter string `json:"cover_letter"` // the draft body, to paste ("" if none)
}

// ApplyResult is the outcome after the window closed.
type ApplyResult struct {
	ApplyPrep
	Submitted bool   `json:"submitted"` // TopCV's history now lists the job
	Confirm   bool   `json:"confirm"`   // a site braveduck can't check: the user says whether they submitted
	Message   string `json:"message"`
}

// openApplyWindow and confirmApplied are swapped in tests.
var openApplyWindow = func(ctx context.Context, s *Service, p platform.Platform, url string) error {
	sess, seed, err := s.PlatformSession(ctx, p.ID)
	if err != nil {
		return err
	}
	return browser.ShowPage(ctx, p, sess, seed, url, 20*time.Minute)
}

var renderCVPDF = browser.RenderPDF

// PrepareApply checks job id can be applied to now and writes its CV PDF.
func (s *Service) PrepareApply(ctx context.Context, id int64) (ApplyPrep, error) {
	if !s.OnOwnComputer() {
		return ApplyPrep{}, ErrHosted
	}
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return ApplyPrep{}, err
	}
	site, err := s.applySite(j)
	if err != nil {
		return ApplyPrep{}, err
	}
	if j.TopcvStatus != "" || j.Status == "applied" || j.Status == "interview" || j.Status == "offer" {
		return ApplyPrep{}, fmt.Errorf("%w: you've already applied for this job (it's in %s)", ErrInvalid, j.Status)
	}
	if !s.platformConnected(ctx, site.ID) {
		return ApplyPrep{}, fmt.Errorf("%w: log in to %s first — braveduck login %s", ErrInvalid, site.Name, site.ID)
	}
	if err := s.checkApplyLimits(ctx); err != nil {
		return ApplyPrep{}, err
	}
	path, err := s.applyCV(ctx, j)
	if err != nil {
		return ApplyPrep{}, err
	}
	prep := ApplyPrep{JobID: id, URL: j.URL, Site: site.Name, SiteID: site.ID, CVPDF: path}
	if d, _, err := s.currentDraft(ctx, id); err == nil {
		prep.CoverLetter = d.Body
	}
	return prep, nil
}

// applyCV is the CV file to upload for job j: its tailored CV printed to
// PDF, or else the user's base CV.
func (s *Service) applyCV(ctx context.Context, j jobs.Job) (string, error) {
	md, _, err := s.CV(ctx, j.ID)
	if err != nil {
		path, err := s.baseCVFile(ctx)
		if err != nil {
			return "", err
		}
		if path == "" {
			return "", fmt.Errorf("%w: upload your CV (📄 My CV) or tailor one for this job first (Ask your AI → tailor)", ErrInvalid)
		}
		return path, nil
	}
	pdf, err := renderCVPDF(ctx, markdown.Page(j.Title, markdown.ToHTML(md)))
	if err != nil {
		return "", fmt.Errorf("couldn't make the CV PDF: %w", err)
	}
	path := filepath.Join(filepath.Dir(s.profilePath), "cvs", fmt.Sprintf("job-%d-v%d.pdf", j.ID, j.CVVersion))
	return path, os.WriteFile(path, pdf, 0o600)
}

// AssistApply runs the whole assisted apply for job id: prepare, open the
// window, wait for the user to close it, then confirm — from TopCV's history,
// or (other sites) by leaving the job asking "did you submit it?"
// (ConfirmApplied answers).
func (s *Service) AssistApply(ctx context.Context, id int64) (ApplyResult, error) {
	prep, err := s.PrepareApply(ctx, id)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := s.reserveApply(ctx); err != nil {
		return ApplyResult{}, err
	}
	return s.assistWith(ctx, id, prep)
}

// applyMu makes checking the apply limits and recording the attempt one
// step, so applies started together can't all pass the cap and cooldown.
var applyMu sync.Mutex

// reserveApply checks the daily cap and cooldown and, if they allow it,
// records this attempt at once (before any window opens).
func (s *Service) reserveApply(ctx context.Context) error {
	applyMu.Lock()
	defer applyMu.Unlock()
	if err := s.checkApplyLimits(ctx); err != nil {
		return err
	}
	return s.st.RecordApplyAttempt(ctx, s.now())
}

// assistWith runs a prepared, reserved apply: the window, then the check.
func (s *Service) assistWith(ctx context.Context, id int64, prep ApplyPrep) (ApplyResult, error) {
	site, err := s.platform(prep.SiteID)
	if err != nil {
		return ApplyResult{}, err
	}
	res := ApplyResult{ApplyPrep: prep}
	_ = s.st.SetApplyState(ctx, id, "filling", "window open — submit on "+site.Name, s.now())
	if err := openApplyWindow(ctx, s, site, prep.URL); err != nil && !errors.Is(err, browser.ErrWindowTimeout) {
		_ = s.st.SetApplyState(ctx, id, "", "", s.now())
		return res, err
	}

	if site.ID != "topcv" {
		// No history braveduck can read on this site: the user says.
		res.Confirm = true
		res.Message = fmt.Sprintf("Did you submit it on %s? Say so on the card.", site.Name)
		_ = s.st.SetApplyState(ctx, id, "confirm", "did you submit it on "+site.Name+"?", s.now())
		return res, nil
	}
	if _, err := s.SyncTopCV(ctx); err != nil {
		res.Message = "Couldn't check TopCV afterwards (" + err.Error() + "); run Sync applications later."
		return res, nil
	}
	// The only trustworthy signal is TopCV's own history listing the job
	// (the sync writes topcv_status for it); a lane move by hand isn't.
	j, _ := s.GetJob(ctx, id)
	res.Submitted = j.TopcvStatus != ""
	if res.Submitted {
		res.Message = "TopCV lists the application — moved to Applied."
		s.logEvent(ctx, "moved", fmt.Sprintf("Applied on TopCV: %q", j.Title))
	} else {
		_ = s.st.SetApplyState(ctx, id, "", "", s.now())
		res.Message = "TopCV doesn't list this application, so nothing changed. Try again when you're ready."
	}
	return res, nil
}

// ConfirmApplied records the user's answer after applying on a site
// braveduck can't check: submitted moves the job to Applied (starting the
// no-answer clock); not submitted just clears the question.
func (s *Service) ConfirmApplied(ctx context.Context, id int64, submitted bool) (jobs.Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return j, err
	}
	if !submitted {
		_ = s.st.SetApplyState(ctx, id, "", "", s.now())
		return s.GetJob(ctx, id)
	}
	site := "the site"
	if p, err := s.applySite(j); err == nil {
		site = p.Name
	}
	if _, err := s.SetStatus(ctx, id, "applied"); err != nil {
		return jobs.Job{}, err
	}
	_ = s.st.SetApplyState(ctx, id, "sent", "applied on "+site+" (you confirmed)", s.now())
	s.logEvent(ctx, "moved", fmt.Sprintf("Applied on %s: %q", site, j.Title))
	return s.GetJob(ctx, id)
}

// applySite is the job site j's link is on, among the platforms braveduck
// can hold a login for.
func (s *Service) applySite(j jobs.Job) (platform.Platform, error) {
	reg, _, err := s.setup()
	if err != nil {
		return platform.Platform{}, err
	}
	if u, err := url.Parse(j.URL); err == nil && u.Hostname() != "" {
		for _, p := range reg.All() {
			if p.Owns(u.Hostname()) {
				return p, nil
			}
		}
	}
	return platform.Platform{}, fmt.Errorf("%w: assisted apply works for jobs on a site you can log in to (%s) — this one is from %s; apply there directly",
		ErrInvalid, strings.Join(reg.IDs(), ", "), orDash(j.DiscoverySource))
}

// platform returns a registered platform by id.
func (s *Service) platform(id string) (platform.Platform, error) {
	reg, _, err := s.setup()
	if err != nil {
		return platform.Platform{}, err
	}
	return reg.Get(id)
}

// checkApplyLimits enforces the daily cap and the cooldown between applies.
func (s *Service) checkApplyLimits(ctx context.Context) error {
	times, err := s.st.ApplyAttempts(ctx, s.now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if len(times) >= DailyApplyCap {
		return fmt.Errorf("%w: %d applications in the last 24 hours (the daily cap) — try again tomorrow", ErrApplyLimit, len(times))
	}
	if len(times) > 0 {
		if cd := apply.CooldownRemaining(times[len(times)-1], s.now()); cd.Remaining > 0 {
			return fmt.Errorf("%w: wait %d more minute(s) between applications", ErrApplyLimit, (cd.Remaining+59)/60)
		}
	}
	return nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "an unknown source"
	}
	return s
}

// StartAssistApply prepares job id and runs the rest of AssistApply in the
// background (the board and agents can't wait on a human). The returned prep
// carries the CV path and cover letter to show the user; the outcome lands in
// the activity feed.
func (s *Service) StartAssistApply(id int64) (ApplyPrep, error) {
	ctx := context.Background()
	prep, err := s.PrepareApply(ctx, id)
	if err != nil {
		return ApplyPrep{}, err
	}
	if err := s.reserveApply(ctx); err != nil {
		return ApplyPrep{}, err
	}
	go func() {
		res, err := s.assistWith(ctx, id, prep)
		switch {
		case err != nil:
			s.logEvent(ctx, "error", fmt.Sprintf("Apply on %s (job #%d): %v", prep.Site, id, err))
		case !res.Submitted && !res.Confirm:
			s.logEvent(ctx, "info", fmt.Sprintf("Apply on %s (job #%d): %s", prep.Site, id, res.Message))
		}
	}()
	return prep, nil
}
