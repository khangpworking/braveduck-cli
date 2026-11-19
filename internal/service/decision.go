package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// Decision models rate jobs and check leads without writing text: Jev
// (TypeSafe AI) or Clef (Cloudflare Workers AI), which share one request and
// answer format. The user picks one in Settings → Rating and may paste a key
// for each. Pasted keys are sealed with the session vault before they reach the
// database and are never returned, logged or shown again.

// Decision providers.
const (
	ProviderJev  = "jev"
	ProviderClef = "clef"
)

// Settings keys. The *_sealed values are vault blobs (hex), never plain keys.
const (
	decisionProviderKey = "decision_provider"
	jevKeySetting       = "jev_key_sealed"
	clefTokenSetting    = "clef_token_sealed"
	clefAccountSetting  = "clef_account_id"
	clefModelSetting    = "clef_model"
)

// Vault associated data, so a sealed key can't be moved to another slot.
const (
	jevKeyAAD    = "decision-key:jev"
	clefTokenAAD = "decision-key:clef"
)

// ErrNoClefKey means Clef was chosen but its token or account ID is missing.
var ErrNoClefKey = fmt.Errorf("%w: Clef needs a Cloudflare Workers AI token and account ID — paste them in Settings → Rating", ErrInvalid)

// KeyState says whether a provider has a key, never the key itself.
type KeyState struct {
	Saved  bool   `json:"saved"`            // pasted in Settings (sealed in the vault)
	Source string `json:"source,omitempty"` // the key in use: "settings", an env var or a file path; "" when none
}

// DecisionSettings is what Settings → Rating shows about the decision models.
type DecisionSettings struct {
	Provider      string   `json:"provider" tstype:"'jev' | 'clef'"`
	Model         string   `json:"model"` // the rater id its verdicts carry
	Jev           KeyState `json:"jev"`
	Clef          KeyState `json:"clef"`
	ClefAccountID string   `json:"clef_account_id,omitempty"` // not a secret
	ClefModel     string   `json:"clef_model" tstype:"'clef-flash' | 'clef'"`
}

// DecisionUpdate changes the decision settings; a nil field is left alone and
// an empty key removes the pasted one.
type DecisionUpdate struct {
	Provider      *string `json:"provider,omitempty" tstype:"'jev' | 'clef'"`
	JevKey        *string `json:"jev_key,omitempty"`
	ClefToken     *string `json:"clef_token,omitempty"`
	ClefAccountID *string `json:"clef_account_id,omitempty"`
	ClefModel     *string `json:"clef_model,omitempty" tstype:"'clef-flash' | 'clef'"`
}

// IsDecisionModel reports whether a rater id belongs to a decision model
// (Jev or Clef), which rates on the board itself instead of through an agent.
func IsDecisionModel(id string) bool { return IsJevModel(id) || jev.IsClefModel(id) }

// decisionProvider is the provider this user picked; Jev until they pick.
func (s *Service) decisionProvider(ctx context.Context) string {
	if v, _, err := s.st.Setting(ctx, decisionProviderKey); err == nil && v == ProviderClef {
		return ProviderClef
	}
	return ProviderJev
}

// clefModel is the Clef model this user rates with (clef-flash by default).
func (s *Service) clefModel(ctx context.Context) string {
	if v, _, err := s.st.Setting(ctx, clefModelSetting); err == nil && jev.IsClefModel(v) {
		return v
	}
	return jev.ClefFlash
}

// decisionModel is the rater id the chosen provider rates as.
func (s *Service) decisionModel(ctx context.Context) string {
	if s.decisionProvider(ctx) == ProviderClef {
		return s.clefModel(ctx)
	}
	return jev.DefaultModel
}

// sealedSetting opens a pasted key; "" when none is saved.
func (s *Service) sealedSetting(ctx context.Context, key, aad string) (string, error) {
	v, ok, err := s.st.Setting(ctx, key)
	if err != nil || !ok || v == "" {
		return "", err
	}
	blob, err := hex.DecodeString(v)
	if err != nil {
		return "", fmt.Errorf("stored %s is unreadable; paste the key again", aad)
	}
	_, vlt, err := s.setup()
	if err != nil {
		return "", err
	}
	plain, err := vlt.Open(aad, blob)
	if err != nil {
		return "", fmt.Errorf("stored %s can't be opened with this machine's session key; paste the key again", aad)
	}
	return string(plain), nil
}

// saveSealed seals value into the vault and stores it; "" removes it.
func (s *Service) saveSealed(ctx context.Context, key, aad, value string) error {
	if value == "" {
		return s.st.SetSetting(ctx, key, "")
	}
	_, vlt, err := s.setup()
	if err != nil {
		return err
	}
	blob, err := vlt.Seal(aad, []byte(value))
	if err != nil {
		return err
	}
	return s.st.SetSetting(ctx, key, hex.EncodeToString(blob))
}

// decisionClient is the client of the chosen provider and the rater id it
// rates as. serverKey is true when the key isn't this user's own (the
// environment's or a key file's, which on a shared server the admin pays for).
func (s *Service) decisionClient(ctx context.Context) (c jevAsker, model string, serverKey bool, err error) {
	model = s.decisionModel(ctx)
	if s.jev != nil { // tests: the fake stands in for the server's key
		return s.jev, model, true, nil
	}
	if s.decisionProvider(ctx) == ProviderClef {
		token, err := s.sealedSetting(ctx, clefTokenSetting, clefTokenAAD)
		if err != nil {
			return nil, model, false, err
		}
		account, _, _ := s.st.Setting(ctx, clefAccountSetting)
		if token == "" || !jev.ValidAccountID(account) {
			return nil, model, false, ErrNoClefKey
		}
		return jev.NewClef(token, account, model), model, false, nil
	}
	key, err := s.sealedSetting(ctx, jevKeySetting, jevKeyAAD)
	if err != nil {
		return nil, model, false, err
	}
	if key != "" {
		return jev.New(key), model, false, nil
	}
	key, _, err = loadJevKey()
	if err != nil {
		return nil, model, false, err
	}
	return jev.New(key), model, true, nil
}

// DecisionSettings reports the provider, its model and where each key comes
// from. It never returns a key.
func (s *Service) DecisionSettings(ctx context.Context) (DecisionSettings, error) {
	out := DecisionSettings{Provider: s.decisionProvider(ctx), Model: s.decisionModel(ctx), ClefModel: s.clefModel(ctx)}
	if v, ok, err := s.st.Setting(ctx, jevKeySetting); err != nil {
		return out, err
	} else if ok && v != "" {
		out.Jev = KeyState{Saved: true, Source: "settings"}
	} else if _, src, err := loadJevKey(); err == nil {
		out.Jev.Source = src
	}
	if v, ok, err := s.st.Setting(ctx, clefTokenSetting); err != nil {
		return out, err
	} else if ok && v != "" {
		out.Clef = KeyState{Saved: true, Source: "settings"}
	}
	out.ClefAccountID, _, _ = s.st.Setting(ctx, clefAccountSetting)
	return out, nil
}

// SetDecisionSettings saves pasted keys (sealed), the Clef account and model,
// and the provider. Switching provider or Clef model moves the decision
// model's place among the raters (default or challenger) to the new model, so
// the board badge follows the switch.
func (s *Service) SetDecisionSettings(ctx context.Context, in DecisionUpdate) (DecisionSettings, error) {
	if in.Provider != nil && *in.Provider != ProviderJev && *in.Provider != ProviderClef {
		return DecisionSettings{}, fmt.Errorf("%w: provider must be %q or %q", ErrInvalid, ProviderJev, ProviderClef)
	}
	if in.ClefModel != nil && !jev.IsClefModel(*in.ClefModel) {
		return DecisionSettings{}, fmt.Errorf("%w: Clef model must be %q or %q", ErrInvalid, jev.ClefFlash, jev.Clef)
	}
	if in.ClefAccountID != nil {
		v := strings.TrimSpace(*in.ClefAccountID)
		if v != "" && !jev.ValidAccountID(v) {
			return DecisionSettings{}, fmt.Errorf("%w: a Cloudflare account ID is 32 letters and digits", ErrInvalid)
		}
		in.ClefAccountID = &v
	}
	for _, k := range []*string{in.JevKey, in.ClefToken} {
		if k != nil && strings.ContainsAny(strings.TrimSpace(*k), " \t\r\n") {
			return DecisionSettings{}, fmt.Errorf("%w: an API key has no spaces or line breaks", ErrInvalid)
		}
	}
	before := s.decisionModel(ctx)
	if in.JevKey != nil {
		if err := s.saveSealed(ctx, jevKeySetting, jevKeyAAD, strings.TrimSpace(*in.JevKey)); err != nil {
			return DecisionSettings{}, err
		}
	}
	if in.ClefToken != nil {
		if err := s.saveSealed(ctx, clefTokenSetting, clefTokenAAD, strings.TrimSpace(*in.ClefToken)); err != nil {
			return DecisionSettings{}, err
		}
	}
	if in.ClefAccountID != nil {
		if err := s.st.SetSetting(ctx, clefAccountSetting, strings.ToLower(*in.ClefAccountID)); err != nil {
			return DecisionSettings{}, err
		}
	}
	if in.ClefModel != nil {
		if err := s.st.SetSetting(ctx, clefModelSetting, *in.ClefModel); err != nil {
			return DecisionSettings{}, err
		}
	}
	if in.Provider != nil {
		if err := s.st.SetSetting(ctx, decisionProviderKey, *in.Provider); err != nil {
			return DecisionSettings{}, err
		}
	}
	if after := s.decisionModel(ctx); after != before {
		if err := s.swapDecisionModel(ctx, after); err != nil {
			return DecisionSettings{}, err
		}
	}
	return s.DecisionSettings(ctx)
}

// swapDecisionModel replaces the decision model among the saved raters with
// model, keeping its role. A board with no saved rater config is left alone:
// the scheduled run enables the new provider when its key is there.
func (s *Service) swapDecisionModel(ctx context.Context, model string) error {
	chosen, err := s.st.RatingConfigChosen(ctx)
	if err != nil || !chosen {
		return err
	}
	cur, err := s.RatingConfig(ctx)
	if err != nil {
		return err
	}
	next := rating.Config{ABEnabled: cur.ABEnabled, DefaultModel: cur.DefaultModel}
	if IsDecisionModel(next.DefaultModel) {
		next.DefaultModel = model
	}
	for _, m := range cur.Models {
		if IsDecisionModel(m) {
			m = model
		}
		if !contains(next.Models, m) {
			next.Models = append(next.Models, m)
		}
	}
	if len(next.Models) < 2 {
		next.ABEnabled = false
	}
	_, err = s.SetRatingConfig(ctx, next)
	return err
}

// decisionKeyAvailable reports whether the chosen provider has a key, without
// building a client (the scheduled run asks this every tick).
func (s *Service) decisionKeyAvailable(ctx context.Context) bool {
	if s.jev != nil {
		return true
	}
	_, _, _, err := s.decisionClient(ctx)
	return err == nil
}

// providerName is how verdict reasons name the model that rated.
func providerName(model string) string {
	if jev.IsClefModel(model) {
		return "Clef"
	}
	return "Jev"
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
