package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
)

// leadJev answers the "kind" question from keywords in the post.
type leadJev struct {
	err   error
	calls int
}

func (f *leadJev) Ask(_ context.Context, state string, qs map[string]jev.Question) (map[string]jev.Answer, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if _, ok := qs["kind"]; !ok {
		panic("not a lead question")
	}
	p := map[string]float64{"job_offer": 0.9, "freelance_gig": 0.04, "not_job": 0.03, "unclear": 0.03}
	choice := "job_offer"
	switch {
	case strings.Contains(state, "Cẩm nang"):
		p = map[string]float64{"job_offer": 0.02, "freelance_gig": 0.01, "not_job": 0.95, "unclear": 0.02}
		choice = "not_job"
	case strings.Contains(state, "maybe"):
		p = map[string]float64{"job_offer": 0.3, "freelance_gig": 0.05, "not_job": 0.6, "unclear": 0.05}
		choice = "not_job"
	}
	return map[string]jev.Answer{"kind": {Choice: choice, Probabilities: p}}, nil
}

func TestLeadCheckOnAdd(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	fj := &leadJev{}
	s.SetJev(fj)

	guide, _ := s.AddJob(ctx, NewJob{Title: "Cẩm nang làm chủ Microsoft Copilot", Source: "facebook", URL: "https://www.facebook.com/groups/1/posts/2/"})
	if guide.Status != "closed" || !strings.Contains(guide.Notes, "not a job offer (95% sure)") {
		t.Fatalf("guide = %q / %q", guide.Status, guide.Notes)
	}
	offer, _ := s.AddJob(ctx, NewJob{Title: "HIRING SENIOR AI ENGINEER", Source: "facebook"})
	if offer.Status != "saved" || offer.Notes != "" {
		t.Fatalf("job offer = %q / %q", offer.Status, offer.Notes)
	}
	unsure, _ := s.AddJob(ctx, NewJob{Title: "maybe a job?", Source: "facebook"})
	if unsure.Status != "saved" {
		t.Fatal("closed at 60% — below the threshold it must be kept")
	}

	// Other sources aren't checked.
	calls := fj.calls
	s.AddJob(ctx, NewJob{Title: "Cẩm nang on VNW", Source: "vietnamworks"})
	if fj.calls != calls {
		t.Fatal("checked a source that isn't on the list")
	}

	// A Jev outage keeps the post.
	fj.err = errors.New("jev: HTTP 503")
	if j, err := s.AddJob(ctx, NewJob{Title: "Cẩm nang again", Source: "facebook"}); err != nil || j.Status != "saved" {
		t.Fatalf("outage: %q %v — a lead must never be lost", j.Status, err)
	}
}

func TestLeadCheckSettingAndRecheck(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if got := s.LeadCheckSources(ctx); len(got) != 1 || got[0] != "facebook" {
		t.Fatalf("default = %v", got)
	}
	s.SetLeadCheck(ctx, "facebook", false)
	j, _ := s.AddJob(ctx, NewJob{Title: "Cẩm nang old post", Source: "facebook"}) // added before Jev
	s.SetJev(&leadJev{})
	s.SetLeadCheck(ctx, "facebook", true)
	s.SetLeadCheck(ctx, "linkedin-groups", true)
	if got := s.LeadCheckSources(ctx); strings.Join(got, ",") != "facebook,linkedin-groups" {
		t.Fatalf("after set = %v", got)
	}
	if _, err := s.SetLeadCheck(ctx, "Bad Name", true); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad source accepted")
	}

	checked, closed, err := s.LeadRecheck(ctx)
	if err != nil || checked != 1 || closed != 1 {
		t.Fatalf("recheck = %d checked, %d closed, %v", checked, closed, err)
	}
	if j, _ = s.GetJob(ctx, j.ID); j.Status != "closed" || !strings.Contains(j.Notes, "lead re-check") {
		t.Fatalf("after recheck = %q %q", j.Status, j.Notes)
	}
}
