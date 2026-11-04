package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// A model id in a copied rating command is one literal argument, even if it
// contains shell metacharacters. The shell only calls this harmless function.
func TestRatingBriefModelArgument(t *testing.T) {
	model := "rival'$(printf expanded)`printf expanded`"
	brief := RenderRatingBrief(RatingBrief{Model: model})
	var command string
	for _, line := range strings.Split(brief, "\n") {
		if strings.HasPrefix(line, "braveduck rate ") {
			command = strings.NewReplacer("<id>", "1", "<1.0-5.0>", "4.2", "<point>", "match").Replace(line)
		}
	}
	if command == "" {
		t.Fatal("rating command missing from brief")
	}
	out, err := exec.Command("sh", "-c", "braveduck() { printf '%s\\0' \"$@\"; };\n"+command).Output()
	if err != nil {
		t.Fatal(err)
	}
	args := bytes.Split(out, []byte{0})
	if len(args) < 5 || string(args[2]) != "--model" || string(args[3]) != model {
		t.Fatalf("copied command changed model argument: %q", args)
	}
}

func TestTemplateCountsAsEmpty(t *testing.T) {
	if profileFilled(profileTemplate) {
		t.Fatal("the blank template counts as a filled profile — rating would run on nothing")
	}
	for _, filled := range []string{
		profileTemplate + "\nSenior nurse, 8 years ICU experience.\n",
		strings.Replace(profileTemplate, "- Target roles / job titles:", "- Target roles / job titles: Backend engineer", 1),
		strings.Replace(profileTemplate, "# Deal-breakers\n\n<!-- Anything that makes a job an automatic no, one per line. -->\n\n-",
			"# Deal-breakers\n\n- No weekend shifts", 1),
		"Kỹ sư dữ liệu, 3 năm kinh nghiệm SQL/Python.", // any language, no template at all
	} {
		if !profileFilled(filled) {
			t.Errorf("real content not detected:\n%s", filled[max(0, len(filled)-120):])
		}
	}
}

func TestTemplateIsUniversal(t *testing.T) {
	// No assumed country, currency or language baked in.
	for _, local := range []string{"VND", "triệu", "Vietnam", "TopCV", "USD", "$", "€"} {
		if strings.Contains(profileTemplate, local) {
			t.Errorf("template mentions %q — it should stay country/currency neutral", local)
		}
	}
}

func newRatingSvc(t *testing.T) (*Service, string) {
	t.Helper()
	s := newSvc(t)
	p := filepath.Join(t.TempDir(), "sub", "profile.md")
	s.SetProfilePath(p)
	return s, p
}

func TestProfileCreatedOncePrivately(t *testing.T) {
	s, p := newRatingSvc(t)
	ctx := context.Background()

	got, err := s.Profile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Filled || got.Text != profileTemplate || got.Path != p {
		t.Fatalf("fresh profile = %+v", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %v, want 0600 (it holds a CV)", fi.Mode().Perm())
	}

	if err := os.WriteFile(p, []byte("My CV"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Profile(ctx); got.Text != "My CV" || !got.Filled {
		t.Fatalf("existing profile overwritten or misread: %+v", got)
	}
}

func TestRatingRefusesEmptyProfile(t *testing.T) {
	s, p := newRatingSvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "X"})

	for _, err := range []error{
		func() error { _, err := s.RatingQueue(ctx, 0); return err }(),
		func() error { _, err := s.RateBrief(ctx, j.ID); return err }(),
	} {
		if !errors.Is(err, ErrProfileEmpty) || !strings.Contains(err.Error(), p) {
			t.Fatalf("got %v, want ErrProfileEmpty naming %s", err, p)
		}
	}
}

func TestRatingQueueAndSubmit(t *testing.T) {
	s, p := newRatingSvc(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("Backend engineer, Go, 5 years."), 0o600); err != nil {
		t.Fatal(err)
	}

	first, _ := s.AddJob(ctx, NewJob{Title: "Oldest", JD: "Go, Postgres"})
	second, _ := s.AddJob(ctx, NewJob{Title: "Second"})
	closed, _ := s.AddJob(ctx, NewJob{Title: "Closed"})
	s.SetStatus(ctx, closed.ID, "closed")

	q, err := s.RatingQueue(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Jobs) != 2 || q.Jobs[0].ID != first.ID || q.Jobs[0].Description != "Go, Postgres" {
		t.Fatalf("queue = %+v, want oldest-first unscored non-closed jobs", q.Jobs)
	}
	if q.Rubric == "" || q.DefaultModel == "" || !strings.Contains(q.Profile, "Backend") {
		t.Fatalf("brief missing parts: %+v", q)
	}

	// Default rater, letter derived from the score: becomes the badge.
	j, err := s.SubmitRating(ctx, Verdict{JobID: first.ID, Score: 4.2, Reasoning: "Go match • 5y fits senior bar"})
	if err != nil {
		t.Fatal(err)
	}
	if j.ScoreStatus != "scored" || j.ScoreLetter != "B" || !j.HasScore || j.Score != 4.2 {
		t.Fatalf("badge not set: %+v", j)
	}

	// A challenger model is stored but doesn't move the badge.
	j, err = s.SubmitRating(ctx, Verdict{JobID: second.ID, Model: "some-other-model", Score: 2.0, Letter: "F", Reasoning: "missing core skills"})
	if err != nil {
		t.Fatal(err)
	}
	if j.ScoreStatus == "scored" {
		t.Fatalf("challenger verdict took the badge: %+v", j)
	}

	if q, _ = s.RatingQueue(ctx, 0); len(q.Jobs) != 1 || q.Jobs[0].ID != second.ID {
		t.Fatalf("queue after rating = %+v, want only the badge-less job", q.Jobs)
	}

	for _, bad := range []Verdict{
		{JobID: first.ID, Score: 7, Reasoning: "out of range score"},
		{JobID: first.ID, Score: 4, Reasoning: "short"},
		{JobID: first.ID, Score: 4, Letter: "Z", Reasoning: "bad letter given here"},
	} {
		if _, err := s.SubmitRating(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("SubmitRating(%+v) = %v, want ErrInvalid", bad, err)
		}
	}
	if _, err := s.SubmitRating(ctx, Verdict{JobID: 999, Score: 4, Reasoning: "no such job here"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown job = %v, want ErrNotFound", err)
	}

	ev, _ := s.Activity(ctx, 1)
	if len(ev) != 1 || ev[0].Type != "rated" {
		t.Fatalf("last event = %+v, want 'rated'", ev)
	}
}

// With Jev as the default, a claude-code challenger verdict is stored but leaves
// the default badge unchanged.
func TestClaudeCodeChallengerKeepsJevBadge(t *testing.T) {
	s, _ := newRatingSvc(t)
	ctx := context.Background()
	if _, err := s.SetRatingConfig(ctx, rating.Config{ABEnabled: true, Models: []string{jev.DefaultModel, "claude-code"}, DefaultModel: jev.DefaultModel}); err != nil {
		t.Fatal(err)
	}
	job, _ := s.AddJob(ctx, NewJob{Title: "Backend"})
	if _, err := s.SubmitRating(ctx, Verdict{JobID: job.ID, Model: jev.DefaultModel, Score: 4.2, Reasoning: "Jev: B • skills: strong"}); err != nil {
		t.Fatal(err)
	}
	j, err := s.SubmitRating(ctx, Verdict{JobID: job.ID, Model: "claude-code", Score: 2.0, Letter: "F", Reasoning: "missing core skills"})
	if err != nil {
		t.Fatal(err)
	}
	if j.ScoreLetter != "B" || j.Score != 4.2 {
		t.Fatalf("claude-code challenger moved the Jev badge: %+v", j)
	}
	if got := j.Ratings["claude-code"]; got.Letter != "F" {
		t.Fatalf("challenger verdict not stored: %+v", j.Ratings)
	}
}

func TestProfileForRaterKeepsOnlyWhatTheUserWrote(t *testing.T) {
	in := strings.Replace(profileTemplate, "- Target roles / job titles:", "- Target roles / job titles: Data analyst", 1)
	in = strings.Replace(in, "# Deal-breakers\n\n<!-- Anything that makes a job an automatic no, one per line. -->\n\n-",
		"# Deal-breakers\n\n- Night shifts", 1)
	want := "# What I'm looking for\n- Target roles / job titles: Data analyst\n\n# Deal-breakers\n- Night shifts"
	if got := profileForRater(in); got != want {
		t.Fatalf("got:\n%s\n\nwant:\n%s", got, want)
	}
	if got := profileForRater(profileTemplate); got != "" {
		t.Fatalf("blank template should reduce to nothing, got:\n%s", got)
	}
}
