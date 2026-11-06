package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

type fakeJev struct {
	err    error
	calls  int
	states []string
}

func (f *fakeJev) Ask(_ context.Context, state string, qs map[string]jev.Question) (map[string]jev.Answer, error) {
	f.calls++
	f.states = append(f.states, state)
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]jev.Answer{
		"fit":         {Choice: "A", Probabilities: map[string]float64{"A": 0.6, "B": 0.4, "C": 0, "D": 0, "F": 0}},
		"skills":      {Choice: "strong"},
		"role":        {Choice: "on_target"},
		"seniority":   {Choice: "unclear"},
		"pay":         {Choice: "below"},
		"dealbreaker": {Choice: "none"},
	}
	for name := range qs {
		if _, ok := out[name]; !ok {
			panic("unexpected question " + name)
		}
	}
	return out, nil
}

func jevSvc(t *testing.T) (*Service, *fakeJev, int64) {
	t.Helper()
	s, id := docSvc(t) // profile filled, one job
	fj := &fakeJev{}
	s.SetJev(fj)
	s.SetRatingConfig(context.Background(), rating.Config{ABEnabled: true, Models: []string{jev.DefaultModel, "claude-opus-5-5"}, DefaultModel: jev.DefaultModel})
	return s, fj, id
}

func TestJevVerdict(t *testing.T) {
	v := JevVerdict(7, map[string]jev.Answer{
		"fit":         {Choice: "A", Probabilities: map[string]float64{"A": 0.6, "B": 0.4}},
		"skills":      {Choice: "strong"},
		"pay":         {Choice: "below"},
		"dealbreaker": {Choice: "some"},
		"seniority":   {Choice: "unclear"},
	})
	// 0.6*4.75 + 0.4*4.2 = 4.53 → 4.5 → A
	if v.Score != 4.5 || v.Letter != "A" || v.Model != jev.DefaultModel {
		t.Fatalf("verdict = %+v", v)
	}
	for _, want := range []string{"Jev: A (60% sure)", "skills: strong match", "pay: below your minimum", "⚠ hits a deal-breaker"} {
		if !strings.Contains(v.Reasoning, want) {
			t.Errorf("reasoning %q lacks %q", v.Reasoning, want)
		}
	}
	if strings.Contains(v.Reasoning, "seniority") {
		t.Error("an 'unclear' answer shouldn't become a reason")
	}
	if _, err := rating.ValidateVerdict(rating.RawVerdict{JobID: 7, Score: v.Score, Letter: v.Letter, Reasoning: v.Reasoning}); err != nil {
		t.Fatalf("verdict fails the standard validation: %v", err)
	}
}

func TestJevRateSetsBadgeAndSendsProfile(t *testing.T) {
	s, fj, id := jevSvc(t)
	ctx := context.Background()
	j, err := s.JevRate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if j.ScoreStatus != "scored" || j.ScoreLetter != "A" || j.Score != 4.5 {
		t.Fatalf("badge = %q %q %v", j.ScoreStatus, j.ScoreLetter, j.Score)
	}
	if st := fj.states[0]; !strings.Contains(st, "Backend engineer") || !strings.Contains(st, "Title: Go Engineer") {
		t.Fatalf("state sent to Jev lacks the profile or job:\n%s", st)
	}
}

func TestJevAutoRateSchedule(t *testing.T) {
	s, fj, _ := jevSvc(t)
	ctx := context.Background()
	s.AddJob(ctx, NewJob{Title: "Second", JD: "Python"})

	res, err := s.Crawl(ctx, JevRateSource)
	if err != nil || res.Added != 2 || fj.calls != 2 {
		t.Fatalf("auto rate = %+v, %v, %d calls", res, err, fj.calls)
	}
	if res, _ := s.Crawl(ctx, JevRateSource); res.Added != 0 || fj.calls != 2 {
		t.Fatal("re-rated already-scored jobs")
	}

	// Not the default rater → does nothing, isn't flagged.
	s.SetRatingConfig(ctx, rating.Config{Models: []string{"claude-opus-5-5"}, DefaultModel: "claude-opus-5-5"})
	s.AddJob(ctx, NewJob{Title: "Third"})
	if res, err := s.Crawl(ctx, JevRateSource); err != nil || res.Added != 0 || fj.calls != 2 {
		t.Fatalf("ran while Jev isn't the rater: %+v %v", res, err)
	}
}

func TestJevErrorsStopAndReport(t *testing.T) {
	s, fj, id := jevSvc(t)
	ctx := context.Background()
	s.AddJob(ctx, NewJob{Title: "Second"})
	fj.err = errors.New("jev: HTTP 401: invalid key")
	if _, err := s.Crawl(ctx, JevRateSource); err == nil {
		t.Fatal("API error not returned")
	}
	if fj.calls != 1 {
		t.Fatalf("kept calling after an API error: %d calls", fj.calls)
	}
	v, _ := s.CrawlStatus(ctx)
	for _, src := range v.Sources {
		if src.Name == JevRateSource && (src.Healthy || !strings.Contains(src.LastError, "401")) {
			t.Fatalf("panel = %+v", src)
		}
	}
	if j, _ := s.GetJob(ctx, id); j.ScoreStatus == "scored" {
		t.Fatal("scored despite the error")
	}
}

// A board with a Jev key but no rater picked yet starts rating with Jev, so a
// normal user never has to copy a prompt into an agent.
func TestAutoEnableJevWhenKeyPresent(t *testing.T) {
	s, _ := newRatingSvc(t)
	ctx := context.Background()
	old := loadJevKey
	t.Cleanup(func() { loadJevKey = old })
	loadJevKey = func() (string, string, error) { return "test-key", "test", nil }

	if !s.autoEnableJev(ctx) {
		t.Fatal("Jev not enabled with a key and no chosen rater")
	}
	cfg, err := s.RatingConfig(ctx)
	if err != nil || !IsJevModel(cfg.DefaultModel) {
		t.Fatalf("rater = %+v, %v; want Jev", cfg, err)
	}
	if chosen, err := s.st.RatingConfigChosen(ctx); err != nil || !chosen {
		t.Fatalf("choice not saved: %v, %v", chosen, err)
	}
	// A second call is stable and doesn't add models.
	before := cfg
	if !s.autoEnableJev(ctx) {
		t.Fatal("second auto-enable lost Jev")
	}
	if after, _ := s.RatingConfig(ctx); len(after.Models) != len(before.Models) || after.DefaultModel != before.DefaultModel {
		t.Fatalf("config changed: %+v -> %+v", before, after)
	}
}

// Without a key nothing changes: the out-of-box default rater stays.
func TestAutoEnableJevNeedsKey(t *testing.T) {
	s, _ := newRatingSvc(t)
	ctx := context.Background()
	if s.autoEnableJev(ctx) {
		t.Fatal("enabled Jev without a key")
	}
	if cfg, _ := s.RatingConfig(ctx); IsJevModel(cfg.DefaultModel) {
		t.Fatalf("rater changed without a key: %+v", cfg)
	}
	if chosen, _ := s.st.RatingConfigChosen(ctx); chosen {
		t.Fatal("a rater choice was saved without a key")
	}
}

// Once the user picks a rater, a Jev key must not take over.
func TestAutoEnableJevLeavesChosenRater(t *testing.T) {
	s, _ := newRatingSvc(t)
	ctx := context.Background()
	if _, err := s.SetRatingConfig(ctx, rating.Config{Models: []string{"claude-opus-5-5"}, DefaultModel: "claude-opus-5-5"}); err != nil {
		t.Fatal(err)
	}
	old := loadJevKey
	t.Cleanup(func() { loadJevKey = old })
	loadJevKey = func() (string, string, error) { return "test-key", "test", nil }

	if s.autoEnableJev(ctx) {
		t.Fatal("overrode the user's chosen rater")
	}
	if cfg, _ := s.RatingConfig(ctx); cfg.DefaultModel != "claude-opus-5-5" {
		t.Fatalf("rater = %+v, want the chosen one", cfg)
	}
}

// The board's Rate button uses Jev at once when it is available and unchosen.
func TestRequestRatingAutoUsesJev(t *testing.T) {
	s, id := docSvc(t) // filled profile, one job
	fj := &fakeJev{}
	s.SetJev(fj)
	out, err := s.RequestRating(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Rated || out.Job == nil || out.Job.ScoreStatus != "scored" || fj.calls != 1 {
		t.Fatalf("rate = %+v after %d Jev calls", out, fj.calls)
	}
	if cfg, _ := s.RatingConfig(context.Background()); !IsJevModel(cfg.DefaultModel) {
		t.Fatalf("rater = %+v, want Jev", cfg)
	}
}

// The scheduled job also turns Jev on for a keyed, unchosen board.
func TestRunJevRateAutoEnables(t *testing.T) {
	s, id := docSvc(t)
	fj := &fakeJev{}
	s.SetJev(fj)
	res, err := s.Crawl(context.Background(), JevRateSource)
	if err != nil || res.Added != 1 || fj.calls != 1 {
		t.Fatalf("auto rate = %+v, %v, %d calls", res, err, fj.calls)
	}
	if j, _ := s.GetJob(context.Background(), id); j.ScoreStatus != "scored" {
		t.Fatalf("job not scored: %+v", j)
	}
	if cfg, _ := s.RatingConfig(context.Background()); !IsJevModel(cfg.DefaultModel) {
		t.Fatalf("rater = %+v, want Jev", cfg)
	}
}

// Opting out stops automatic Jev rating for that user only: on a shared
// server each user has their own database, so another user's board keeps
// rating with Jev.
func TestJevOptOutIsPerUser(t *testing.T) {
	ctx := context.Background()
	alice, aliceID := docSvc(t)
	alice.SetHosted(HostedInfo{User: "alice"})
	aliceJev := &fakeJev{}
	alice.SetJev(aliceJev)
	bob, bobID := docSvc(t)
	bob.SetHosted(HostedInfo{User: "bob"})
	bobJev := &fakeJev{}
	bob.SetJev(bobJev)

	if err := alice.SetJevOptOut(ctx, true); err != nil {
		t.Fatal(err)
	}
	if out, err := alice.JevOptedOut(ctx); err != nil || !out {
		t.Fatalf("alice opt-out not remembered: %v, %v", out, err)
	}
	if out, _ := bob.JevOptedOut(ctx); out {
		t.Fatal("alice's opt-out leaked to bob")
	}

	if res, err := alice.Crawl(ctx, JevRateSource); err != nil || res.Added != 0 || aliceJev.calls != 0 {
		t.Fatalf("opted-out auto rate = %+v, %v, %d calls", res, err, aliceJev.calls)
	}
	if j, _ := alice.GetJob(ctx, aliceID); j.ScoreStatus == "scored" {
		t.Fatal("opted-out job was scored by Jev")
	}
	if cfg, _ := alice.RatingConfig(ctx); IsJevModel(cfg.DefaultModel) {
		t.Fatalf("opt-out still made Jev the rater: %+v", cfg)
	}
	if _, err := alice.JevRate(ctx, aliceID); !errors.Is(err, ErrJevOptedOut) || aliceJev.calls != 0 {
		t.Fatalf("JevRate after opt-out = %v, %d calls", err, aliceJev.calls)
	}

	if res, err := bob.Crawl(ctx, JevRateSource); err != nil || res.Added != 1 || bobJev.calls != 1 {
		t.Fatalf("bob auto rate = %+v, %v, %d calls", res, err, bobJev.calls)
	}
	if j, _ := bob.GetJob(ctx, bobID); j.ScoreStatus != "scored" {
		t.Fatalf("bob's job not scored: %+v", j)
	}

	// Turning it back on lets Jev rate alice's jobs again.
	if err := alice.SetJevOptOut(ctx, false); err != nil {
		t.Fatal(err)
	}
	if res, err := alice.Crawl(ctx, JevRateSource); err != nil || res.Added != 1 || aliceJev.calls != 1 {
		t.Fatalf("opted back in auto rate = %+v, %v, %d calls", res, err, aliceJev.calls)
	}
}

// Opting out also stops a board where Jev is already the rater.
func TestJevOptOutStopsEnabledJev(t *testing.T) {
	s, fj, id := jevSvc(t)
	ctx := context.Background()
	if err := s.SetJevOptOut(ctx, true); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Crawl(ctx, JevRateSource); err != nil || res.Added != 0 || fj.calls != 0 {
		t.Fatalf("auto rate after opt-out = %+v, %v, %d calls", res, err, fj.calls)
	}
	// Rate falls back to the challenger Jev kept, through the "Ask your AI" prompt.
	out, err := s.RequestRating(ctx, id, "")
	if err != nil || out.Rated || out.Model != "claude-opus-5-5" || !strings.Contains(out.Prompt, `"claude-opus-5-5"`) || fj.calls != 0 {
		t.Fatalf("Rate after opt-out = %+v, %v, %d Jev calls", out, err, fj.calls)
	}
	// That rater's verdict owns the grade.
	j, err := s.SubmitRating(ctx, Verdict{JobID: id, Model: "claude-opus-5-5", Score: 4.2, Reasoning: "good Go match"})
	if err != nil || j.ScoreStatus != "scored" || fj.calls != 0 {
		t.Fatalf("fallback verdict = %+v, %v", j, err)
	}
}
