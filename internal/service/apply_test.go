package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/topcv"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func applySvc(t *testing.T) (*Service, int64) {
	t.Helper()
	s, _ := docSvc(t)
	ctx := context.Background()
	s.st.SaveSession(ctx, store.PlatformSession{Platform: "topcv", Enc: []byte{1}, CreatedAt: "x", FPSeed: "1", Status: store.SessionConnected})
	s.SetDataDir(dirOf(s.profilePath))
	j, _ := s.AddJob(ctx, NewJob{Title: "Data Analyst", URL: "https://www.topcv.vn/viec-lam/data-analyst/2200001.html"})
	s.SubmitCV(ctx, j.ID, "# A\n\n## Experience\n- Data analysis with SQL and Python for five years")
	s.SubmitDraft(ctx, j.ID, Draft{To: "hr@x.vn", Subject: "Application", Body: "Hello, here is my application letter."})

	origPDF, origWin, origFetch := renderCVPDF, openApplyWindow, fetchApplications
	t.Cleanup(func() { renderCVPDF, openApplyWindow, fetchApplications = origPDF, origWin, origFetch })
	renderCVPDF = func(context.Context, string) ([]byte, error) { return []byte("%PDF fake"), nil }
	return s, j.ID
}

func dirOf(p string) string { return p[:strings.LastIndex(p, "/")] }

func TestAssistApplyConfirmsFromHistory(t *testing.T) {
	s, id := applySvc(t)
	ctx := context.Background()
	opened := ""
	openApplyWindow = func(_ context.Context, _ *Service, url string) error { opened = url; return nil }

	// The user closes the window without submitting: nothing changes.
	fetchApplications = func(context.Context, *Service) ([]topcv.Application, error) { return nil, nil }
	res, err := s.AssistApply(ctx, id)
	if err != nil || res.Submitted || !strings.Contains(opened, "2200001") {
		t.Fatalf("not submitted: res=%+v err=%v opened=%q", res, err, opened)
	}
	if fi, err := os.Stat(res.CVPDF); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("CV PDF %q missing or not 0600", res.CVPDF)
	}
	if res.CoverLetter != "Hello, here is my application letter." {
		t.Fatalf("cover letter = %q", res.CoverLetter)
	}
	if j, _ := s.GetJob(ctx, id); j.Status == "applied" {
		t.Fatal("moved to Applied without TopCV listing it")
	}

	// Cooldown between applies.
	if _, err := s.AssistApply(ctx, id); !errors.Is(err, ErrApplyLimit) {
		t.Fatalf("second apply within the cooldown = %v", err)
	}

	// After the cooldown, the user submits: TopCV lists it → Applied.
	clock := time.Now().Add(10 * time.Minute)
	s.now = func() time.Time { return clock }
	fetchApplications = func(context.Context, *Service) ([]topcv.Application, error) {
		return []topcv.Application{{JobID: "2200001", Title: "Data Analyst"}}, nil
	}
	res, err = s.AssistApply(ctx, id)
	if err != nil || !res.Submitted {
		t.Fatalf("submitted: %+v %v", res, err)
	}
	if j, _ := s.GetJob(ctx, id); j.Status != "applied" {
		t.Fatalf("status = %q", j.Status)
	}
	if _, err := s.PrepareApply(ctx, id); !errors.Is(err, ErrInvalid) {
		t.Fatal("offered to apply again to a job TopCV already lists")
	}
}

func TestAssistApplyPreconditions(t *testing.T) {
	s, _ := applySvc(t)
	ctx := context.Background()
	other, _ := s.AddJob(ctx, NewJob{Title: "VNW job", URL: "https://www.vietnamworks.com/x-1-jv"})
	if _, err := s.PrepareApply(ctx, other.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "TopCV jobs") {
		t.Fatalf("non-TopCV job = %v", err)
	}
	nocv, _ := s.AddJob(ctx, NewJob{Title: "No CV", URL: "https://www.topcv.vn/viec-lam/x/2200002.html"})
	if _, err := s.PrepareApply(ctx, nocv.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "upload your CV") {
		t.Fatalf("no CV = %v", err)
	}
	for i := 0; i < DailyApplyCap; i++ {
		s.st.RecordApplyAttempt(ctx, time.Now().Add(-time.Duration(DailyApplyCap-i)*time.Hour))
	}
	j, _ := s.AddJob(ctx, NewJob{Title: "Cap", URL: "https://www.topcv.vn/viec-lam/x/2200003.html"})
	s.SubmitCV(ctx, j.ID, "# A\n\n## Experience\n- long enough CV body for the length check here")
	if _, err := s.PrepareApply(ctx, j.ID); !errors.Is(err, ErrApplyLimit) || !strings.Contains(err.Error(), "daily cap") {
		t.Fatalf("daily cap = %v", err)
	}
	s.st.DeleteSession(ctx, "topcv")
	if _, err := s.PrepareApply(ctx, j.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "log in to TopCV") {
		t.Fatalf("not logged in = %v", err)
	}
}
