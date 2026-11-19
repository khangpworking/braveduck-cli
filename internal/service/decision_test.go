package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

const testAccount = "bd23a6717746b9b4ebb6f4c3c84727c7"

// decisionSvc has a data dir (so the vault can seal keys), a profile and one
// job. A session.key file is written first so the vault never touches the
// machine's real keychain.
func decisionSvc(t *testing.T) (*Service, int64) {
	t.Helper()
	s, p := newRatingSvc(t)
	dir := t.TempDir()
	key := make([]byte, 32)
	rand.Read(key)
	if err := os.WriteFile(filepath.Join(dir, "session.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetDataDir(dir)
	s.SetProfilePath(p)
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("Backend engineer, 5 years Go and Postgres."), 0o600)
	j, err := s.AddJob(context.Background(), NewJob{Title: "Go Engineer", Company: "Duck Co", JD: "Go, Postgres."})
	if err != nil {
		t.Fatal(err)
	}
	return s, j.ID
}

func TestDecisionKeysAreSealedAndNeverReturned(t *testing.T) {
	s, _ := decisionSvc(t)
	ctx := context.Background()
	got, err := s.SetDecisionSettings(ctx, DecisionUpdate{
		JevKey: ptr(" jev-secret-111 "), ClefToken: ptr("cf-secret-222"), ClefAccountID: ptr(strings.ToUpper(testAccount)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Jev.Saved || !got.Clef.Saved || got.ClefAccountID != testAccount || got.Provider != ProviderJev {
		t.Fatalf("settings = %+v", got)
	}
	out, _ := json.Marshal(got)
	for _, k := range []string{jevKeySetting, clefTokenSetting} {
		raw, _, _ := s.st.Setting(ctx, k)
		out = append(out, raw...)
	}
	if strings.Contains(string(out), "secret-111") || strings.Contains(string(out), "secret-222") {
		t.Fatalf("a key leaked into the settings reply or the database: %s", out)
	}

	c, model, serverKey, err := s.decisionClient(ctx)
	if err != nil || model != jev.DefaultModel || serverKey || c.(*jev.Client).Key != "jev-secret-111" {
		t.Fatalf("jev client = %v %q server=%v", err, model, serverKey)
	}
	s.SetDecisionSettings(ctx, DecisionUpdate{Provider: ptr(ProviderClef)})
	c, model, serverKey, err = s.decisionClient(ctx)
	cl, _ := c.(*jev.Client)
	if err != nil || model != jev.ClefFlash || serverKey || cl.Key != "cf-secret-222" || !strings.Contains(cl.Endpoint, testAccount) {
		t.Fatalf("clef client = %v %q server=%v", err, model, serverKey)
	}

	got, _ = s.SetDecisionSettings(ctx, DecisionUpdate{ClefToken: ptr("")})
	if got.Clef.Saved {
		t.Fatal("clearing the Clef token left it saved")
	}
	if _, _, _, err := s.decisionClient(ctx); !errors.Is(err, ErrNoClefKey) {
		t.Fatalf("clef without a token = %v, want ErrNoClefKey", err)
	}
}

func TestSwitchingProviderMovesTheRater(t *testing.T) {
	s, _ := decisionSvc(t)
	ctx := context.Background()
	s.SetRatingConfig(ctx, rating.Config{ABEnabled: true, Models: []string{jev.DefaultModel, "claude-opus-5-5"}, DefaultModel: jev.DefaultModel})

	steps := []struct {
		in      DecisionUpdate
		def     string
		models  []string
		comment string
	}{
		{DecisionUpdate{Provider: ptr(ProviderClef)}, jev.ClefFlash, []string{jev.ClefFlash, "claude-opus-5-5"}, "to clef"},
		{DecisionUpdate{ClefModel: ptr(jev.Clef)}, jev.Clef, []string{jev.Clef, "claude-opus-5-5"}, "careful clef"},
		{DecisionUpdate{Provider: ptr(ProviderJev)}, jev.DefaultModel, []string{jev.DefaultModel, "claude-opus-5-5"}, "back to jev"},
	}
	for _, st := range steps {
		if _, err := s.SetDecisionSettings(ctx, st.in); err != nil {
			t.Fatalf("%s: %v", st.comment, err)
		}
		cfg, _ := s.RatingConfig(ctx)
		if cfg.DefaultModel != st.def || strings.Join(cfg.Models, ",") != strings.Join(st.models, ",") || !cfg.ABEnabled {
			t.Fatalf("%s: config = %+v", st.comment, cfg)
		}
	}

	// A challenger keeps its role.
	s.SetRatingConfig(ctx, rating.Config{ABEnabled: true, Models: []string{"claude-opus-5-5", jev.DefaultModel}, DefaultModel: "claude-opus-5-5"})
	s.SetDecisionSettings(ctx, DecisionUpdate{Provider: ptr(ProviderClef)})
	if cfg, _ := s.RatingConfig(ctx); cfg.DefaultModel != "claude-opus-5-5" || cfg.Models[1] != jev.Clef {
		t.Fatalf("challenger switch: %+v", cfg)
	}
}

func TestClefRatesWithItsOwnModelAndNoServerCap(t *testing.T) {
	s, id := decisionSvc(t)
	ctx := context.Background()
	s.SetDecisionSettings(ctx, DecisionUpdate{Provider: ptr(ProviderClef)})
	if _, err := s.JevRate(ctx, id); !errors.Is(err, ErrNoClefKey) {
		t.Fatalf("clef without a key = %v, want ErrNoClefKey", err)
	}

	v := decisionVerdict(id, jev.ClefFlash, map[string]jev.Answer{
		"fit":    {Choice: "B", Probabilities: map[string]float64{"A": 0, "B": 1, "C": 0, "D": 0, "F": 0}},
		"skills": {Choice: "strong"},
	})
	if v.Model != jev.ClefFlash || !strings.HasPrefix(v.Reasoning, "Clef: B") {
		t.Fatalf("clef verdict = %+v", v)
	}

	// The server's key (environment or file) is capped on a shared server; a
	// key the user pasted is their own and isn't.
	s.SetDecisionSettings(ctx, DecisionUpdate{Provider: ptr(ProviderJev)})
	old := loadJevKey
	t.Cleanup(func() { loadJevKey = old })
	loadJevKey = func() (string, string, error) { return "server-key", "TYPESAFE_API_KEY", nil }
	if _, _, serverKey, err := s.decisionClient(ctx); err != nil || !serverKey {
		t.Fatalf("env key: server=%v err=%v", serverKey, err)
	}
	if got, _ := s.DecisionSettings(ctx); got.Jev.Saved || got.Jev.Source != "TYPESAFE_API_KEY" {
		t.Fatalf("env key state = %+v", got.Jev)
	}
	s.SetDecisionSettings(ctx, DecisionUpdate{JevKey: ptr("mine")})
	if _, _, serverKey, err := s.decisionClient(ctx); err != nil || serverKey {
		t.Fatalf("pasted key: server=%v err=%v", serverKey, err)
	}
}

func TestSetDecisionSettingsRejectsBadInput(t *testing.T) {
	s, _ := decisionSvc(t)
	ctx := context.Background()
	for name, in := range map[string]DecisionUpdate{
		"provider":   {Provider: ptr("gpt")},
		"account":    {ClefAccountID: ptr("not-an-account")},
		"model":      {ClefModel: ptr("clef-pro")},
		"key spaces": {JevKey: ptr("two words")},
	} {
		if _, err := s.SetDecisionSettings(ctx, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if got, _ := s.DecisionSettings(ctx); got.Provider != ProviderJev || got.Jev.Saved || got.ClefModel != jev.ClefFlash {
		t.Fatalf("a rejected update changed something: %+v", got)
	}
}
