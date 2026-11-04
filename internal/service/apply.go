package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/apply"
	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/markdown"
	"github.com/khangpworking/braveduck-cli/internal/core/topcv"
)

// Assisted apply: the user approves each application by starting it, and
// submits it themselves. braveduck prepares the CV PDF, opens a visible
// window on the job with their login, and afterwards checks TopCV's own
// application history to confirm. It never clicks TopCV's submit button and
// never fills the form: TopCV's submit call is undocumented (never exercised
// in topcv-api-analysis), so automating it could send a wrong application
// under the user's name.

// DailyApplyCap limits assisted applications per day, to protect the account.
const DailyApplyCap = 10

// ErrApplyLimit means the daily cap or the cooldown between applies is in effect.
var ErrApplyLimit = errors.New("apply limit")

// ApplyPrep is everything the user needs before the window opens.
type ApplyPrep struct {
	JobID       int64  `json:"job_id"`
	URL         string `json:"url"`
	CVPDF       string `json:"cv_pdf"`       // the CV to pick in TopCV's upload dialog: the tailored one as PDF, else the base CV (PDF or .docx)
	CoverLetter string `json:"cover_letter"` // the draft body, to paste ("" if none)
}

// ApplyResult is the outcome after the window closed.
type ApplyResult struct {
	ApplyPrep
	Submitted bool   `json:"submitted"` // TopCV's history now lists the job
	Message   string `json:"message"`
}

// openApplyWindow and confirmApplied are swapped in tests.
var openApplyWindow = func(ctx context.Context, s *Service, url string) error {
	reg, _, err := s.setup()
	if err != nil {
		return err
	}
	p, _ := reg.Get("topcv")
	sess, seed, err := s.PlatformSession(ctx, p.ID)
	if err != nil {
		return err
	}
	return browser.ShowPage(ctx, p, sess, seed, url, 20*time.Minute)
}

var renderCVPDF = browser.RenderPDF

// PrepareApply checks job id can be applied to now and writes its CV PDF.
func (s *Service) PrepareApply(ctx context.Context, id int64) (ApplyPrep, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return ApplyPrep{}, err
	}
	if topcv.JobIDFromURL(j.URL) == "" {
		return ApplyPrep{}, fmt.Errorf("%w: assisted apply works for TopCV jobs (this one is from %s) — apply on the site directly", ErrInvalid, orDash(j.DiscoverySource))
	}
	if j.TopcvStatus != "" {
		return ApplyPrep{}, fmt.Errorf("%w: TopCV already lists your application for this job", ErrInvalid)
	}
	if !s.platformConnected(ctx, "topcv") {
		return ApplyPrep{}, fmt.Errorf("%w: log in to TopCV first — braveduck login topcv", ErrInvalid)
	}
	if err := s.checkApplyLimits(ctx); err != nil {
		return ApplyPrep{}, err
	}
	path, err := s.applyCV(ctx, j)
	if err != nil {
		return ApplyPrep{}, err
	}
	prep := ApplyPrep{JobID: id, URL: j.URL, CVPDF: path}
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
// window, wait for the user to close it, then confirm from TopCV's history.
func (s *Service) AssistApply(ctx context.Context, id int64) (ApplyResult, error) {
	prep, err := s.PrepareApply(ctx, id)
	if err != nil {
		return ApplyResult{}, err
	}
	res := ApplyResult{ApplyPrep: prep}
	_ = s.st.SetApplyState(ctx, id, "filling", "window open — submit on TopCV", s.now())
	if err := openApplyWindow(ctx, s, prep.URL); err != nil && !errors.Is(err, browser.ErrWindowTimeout) {
		_ = s.st.SetApplyState(ctx, id, "", "", s.now())
		return res, err
	}
	_ = s.st.RecordApplyAttempt(ctx, s.now())

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
	go func() {
		res, err := s.AssistApply(ctx, id)
		switch {
		case err != nil:
			s.logEvent(ctx, "error", fmt.Sprintf("Apply on TopCV (job #%d): %v", id, err))
		case !res.Submitted:
			s.logEvent(ctx, "info", fmt.Sprintf("Apply on TopCV (job #%d): %s", id, res.Message))
		}
	}()
	return prep, nil
}
