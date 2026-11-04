package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/topcv"
)

func TestSyncTopCV(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	saved, _ := s.AddJob(ctx, NewJob{Title: "Senior Marketing Executive", URL: "https://www.topcv.vn/viec-lam/senior-marketing-executive/2193898.html"})
	interview, _ := s.AddJob(ctx, NewJob{Title: "MarTech", URL: "https://www.topcv.vn/brand/vus/tuyen-dung/martech-j2190001.html"})
	s.SetStatus(ctx, interview.ID, "interview")

	orig := fetchApplications
	t.Cleanup(func() { fetchApplications = orig })
	fetchApplications = func(context.Context, *Service) ([]topcv.Application, error) {
		return []topcv.Application{
			{JobID: "2193898", Title: "Senior Marketing Executive", StatusText: "TopCV đã nhắc NTD", StatusTime: "22-06-2026"},
			{JobID: "2190001", Title: "MarTech", Viewed: true, StatusText: "NTD đã xem CV"},
			{JobID: "2176176", Title: "AI Content Operator", Company: "Vinh Cara",
				URL: "https://www.topcv.vn/viec-lam/ai-content-operator/2176176.html", AppliedAt: "2026-06-12 22:46:00", Viewed: true},
		}, nil
	}

	res, err := s.SyncTopCV(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Found != 3 || res.Added != 1 || res.Moved != 1 || res.Updated != 2 {
		t.Fatalf("result = %+v", res)
	}
	j, _ := s.GetJob(ctx, saved.ID)
	if j.Status != "applied" || j.TopcvStatus != topcv.StatusSent || !strings.Contains(j.ApplyDetail, "(22-06-2026)") {
		t.Fatalf("saved job after sync = status %q topcv %q detail %q", j.Status, j.TopcvStatus, j.ApplyDetail)
	}
	if !strings.HasPrefix(j.ExpiresAt, "applied:") {
		t.Fatalf("moved to Applied without starting the no-answer clock: expires_at %q", j.ExpiresAt)
	}
	if j, _ := s.GetJob(ctx, interview.ID); j.Status != "interview" || j.TopcvStatus != topcv.StatusSeen {
		t.Fatalf("a job already past Applied must not move back: %q / %q", j.Status, j.TopcvStatus)
	}
	added, _ := s.ListJobs(ctx, ListFilter{Status: "applied", Source: "topcv"})
	var found bool
	for _, a := range added {
		if a.Title == "AI Content Operator" {
			found = a.ExpiresAt == "applied:2026-06-12" && a.TopcvStatus == topcv.StatusSeen
		}
	}
	if !found {
		t.Fatalf("missing application not added correctly: %+v", added)
	}

	// Idempotent: a second sync changes nothing.
	if res, _ := s.SyncTopCV(ctx); res.Added+res.Moved+res.Updated != 0 {
		t.Fatalf("second sync = %+v", res)
	}

	fetchApplications = func(context.Context, *Service) ([]topcv.Application, error) {
		return nil, errors.New("timeout")
	}
	if _, err := s.SyncTopCV(ctx); err == nil {
		t.Fatal("failure not returned")
	}
	v, _ := s.CrawlStatus(ctx)
	for _, src := range v.Sources {
		if src.Name == "topcv-sync" && src.Healthy {
			t.Fatal("failed sync shown as healthy")
		}
	}
}

// An application to a job the user pasted without its TopCV link updates
// that card instead of adding a second one.
func TestSyncTopCVMatchesPastedJob(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	pasted, _ := s.AddFromContent(ctx, "AI Content Operator\nVinh Cara\nLương: 12 triệu")

	orig := fetchApplications
	t.Cleanup(func() { fetchApplications = orig })
	fetchApplications = func(context.Context, *Service) ([]topcv.Application, error) {
		return []topcv.Application{{JobID: "2176176", Title: "AI Content Operator", Company: "Vinh Cara",
			URL: "https://www.topcv.vn/viec-lam/ai-content-operator/2176176.html", Viewed: true}}, nil
	}
	res, err := s.SyncTopCV(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Moved != 1 {
		t.Fatalf("result = %+v, want the pasted card moved, nothing added", res)
	}
	if j, _ := s.GetJob(ctx, pasted.ID); j.Status != "applied" || j.TopcvStatus != topcv.StatusSeen {
		t.Fatalf("pasted card = %q / %q", j.Status, j.TopcvStatus)
	}
}
