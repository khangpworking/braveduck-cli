package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/platform"
	"github.com/khangpworking/braveduck-cli/internal/core/vault"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Platform sessions (docs/specs/platform-sessions.md): the user logs in to a
// job site in a real browser window; braveduck keeps the session, sealed by
// the vault, and reuses it headlessly. No password is ever seen or stored, and
// no cookie value ever leaves this package except into the vault.

// ErrStillLoggedOut means the login window closed but the site still treats
// the session as logged out, so nothing was saved.
var ErrStillLoggedOut = errors.New("the site still shows you as logged out — nothing was saved; try again")

// ErrLoginInProgress means a login window for that platform is already open.
var ErrLoginInProgress = errors.New("a login window for this site is already open")

// platformDeps are the moving parts, swapped in tests.
type platformDeps struct {
	login func(ctx context.Context, p platform.Platform, seed string, o browser.LoginOptions) (browser.Session, error)
	probe func(ctx context.Context, p platform.Platform, s browser.Session, seed string) (browser.ProbeResult, error)
}

// platformState is the service's platform machinery, set up by SetDataDir.
type platformState struct {
	dir  string
	deps platformDeps

	once  sync.Once
	reg   *platform.Registry
	vault *vault.Vault
	err   error

	mu      sync.Mutex
	loading map[string]bool // platform id → login window open in this process
}

// SetDataDir points the service at braveduck's data dir (where the database
// lives): platforms.json, the session key and the profile are found there.
func (s *Service) SetDataDir(dir string) {
	s.profilePath = filepath.Join(dir, "profile.md")
	s.plat = &platformState{
		dir:     dir,
		deps:    platformDeps{login: browser.Login, probe: browser.Probe},
		loading: map[string]bool{},
	}
}

// setup loads the registry and the vault on first use (opening the vault may
// create the key, so it's deferred until sessions are actually touched).
func (s *Service) setup() (*platform.Registry, *vault.Vault, error) {
	if s.plat == nil {
		return nil, nil, errors.New("no data dir configured")
	}
	ps := s.plat
	ps.once.Do(func() {
		ps.reg, ps.err = platform.Load(filepath.Join(ps.dir, "platforms.json"))
		if ps.err == nil {
			if s.hosted != nil {
				ps.vault, ps.err = vault.OpenFile(ps.dir)
			} else {
				ps.vault, ps.err = vault.Open(ps.dir)
			}
		}
	})
	return ps.reg, ps.vault, ps.err
}

// PlatformStatus is what anyone may see about a platform: never cookie values.
type PlatformStatus struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"` // not_connected / connecting / connected / expired / unknown
	Since       string   `json:"since,omitempty"`
	CheckedAt   string   `json:"checked_at,omitempty"`
	ExpiresHint string   `json:"expires_hint,omitempty"`
	Account     string   `json:"account,omitempty"` // masked, e.g. "ng…@gmail.com"
	Verified    bool     `json:"verified"`          // its logged-in check is confirmed against the real site
	Builtin     bool     `json:"builtin"`
	Domains     []string `json:"domains"` // the site's hosts (and their subdomains), e.g. to match a job's link
}

const (
	statusNotConnected = "not_connected"
	statusConnecting   = "connecting"
)

// Platforms lists every known platform with its session status.
func (s *Service) Platforms(ctx context.Context) ([]PlatformStatus, error) {
	reg, _, err := s.setup()
	if err != nil {
		return nil, err
	}
	saved, err := s.st.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.PlatformSession{}
	for _, ps := range saved {
		byID[ps.Platform] = ps
	}
	var out []PlatformStatus
	for _, p := range reg.All() {
		st := PlatformStatus{ID: p.ID, Name: p.Name, Status: statusNotConnected, Verified: p.Verified, Builtin: p.Builtin, Domains: p.Domains}
		if ps, ok := byID[p.ID]; ok {
			st.Status, st.Since, st.CheckedAt, st.ExpiresHint, st.Account = ps.Status, ps.CreatedAt, ps.CheckedAt, ps.ExpiresHint, ps.AccountHint
		}
		if s.loginOpen(p.ID) {
			st.Status = statusConnecting
		}
		out = append(out, st)
	}
	return out, nil
}

// LoginHooks lets the CLI react during a login.
type LoginHooks struct {
	OnStart    func(p platform.Platform) // the site is valid; the window is about to open
	OnDetected func()
}

// Connect opens the login window for platform id, waits for the user to log
// in, verifies the session with a headless check, and saves it encrypted.
// A re-login keeps the platform's fingerprint seed, so the account keeps
// seeing one device.
func (s *Service) Connect(ctx context.Context, id string, h LoginHooks) (PlatformStatus, error) {
	if !s.OnOwnComputer() {
		return PlatformStatus{}, ErrHosted
	}
	reg, v, err := s.setup()
	if err != nil {
		return PlatformStatus{}, err
	}
	p, err := reg.Get(id)
	if err != nil {
		return PlatformStatus{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if !s.claimLogin(p.ID) {
		return PlatformStatus{}, ErrLoginInProgress
	}
	if h.OnStart != nil {
		h.OnStart(p)
	}
	err = s.connect(ctx, p, v, h)
	s.releaseLogin(p.ID) // before reading the status, or it would still say "connecting"
	if err != nil {
		return PlatformStatus{}, err
	}
	return s.platformStatus(ctx, p.ID)
}

// connect does the work of Connect; the caller holds the platform's login slot.
func (s *Service) connect(ctx context.Context, p platform.Platform, v *vault.Vault, h LoginHooks) error {
	seed := browser.NewSeed()
	if old, err := s.st.GetSession(ctx, p.ID); err == nil && old.FPSeed != "" {
		seed = old.FPSeed
	}

	sess, err := s.plat.deps.login(ctx, p, seed, browser.LoginOptions{OnDetected: h.OnDetected})
	if err != nil {
		return err
	}
	res, err := s.plat.deps.probe(ctx, p, sess, seed)
	state := res.State
	if err != nil {
		state = store.SessionUnknown
	}
	if state == store.SessionExpired {
		return ErrStillLoggedOut
	}
	if sess.Account == "" {
		sess.Account = res.Account // the login page didn't carry it; the account page did
	}

	plain, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	enc, err := v.Seal(p.ID, plain)
	if err != nil {
		return err
	}
	now := s.now().UTC().Format(time.RFC3339)
	if err := s.st.SaveSession(ctx, store.PlatformSession{
		Platform: p.ID, Enc: enc, CreatedAt: now, ExpiresHint: sess.ExpiresHint(p),
		FPSeed: seed, Status: state, CheckedAt: now, AccountHint: sess.Account,
	}); err != nil {
		return err
	}
	s.logEvent(ctx, "connected", fmt.Sprintf("Connected %s", p.Name))
	return nil
}

// StartConnect runs Connect in the background (for the board and agents, whose
// requests can't wait minutes for a human). The outcome lands in the activity
// feed; Platforms shows "connecting" meanwhile.
func (s *Service) StartConnect(id string) (PlatformStatus, error) {
	if !s.OnOwnComputer() {
		return PlatformStatus{}, ErrHosted
	}
	reg, v, err := s.setup()
	if err != nil {
		return PlatformStatus{}, err
	}
	p, err := reg.Get(id)
	if err != nil {
		return PlatformStatus{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	// Claim before returning, so a second click can't open a second window.
	if !s.claimLogin(p.ID) {
		return PlatformStatus{}, ErrLoginInProgress
	}
	go func() {
		defer s.releaseLogin(p.ID)
		ctx := context.Background()
		if err := s.connect(ctx, p, v, LoginHooks{}); err != nil {
			s.logEvent(ctx, "error", fmt.Sprintf("Login to %s failed: %v", p.Name, err))
		}
	}()
	return PlatformStatus{ID: p.ID, Name: p.Name, Status: statusConnecting, Verified: p.Verified, Builtin: p.Builtin}, nil
}

// Disconnect deletes a platform's session. deleted is false if there was none.
func (s *Service) Disconnect(ctx context.Context, id string) (deleted bool, err error) {
	reg, _, err := s.setup()
	if err != nil {
		return false, err
	}
	p, err := reg.Get(id)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	deleted, err = s.st.DeleteSession(ctx, p.ID)
	if deleted {
		s.logEvent(ctx, "disconnected", fmt.Sprintf("Disconnected %s", p.Name))
	}
	return deleted, err
}

// CheckPlatform re-runs the logged-in check on a saved session and records it.
// A session the vault can't open (key changed, tampered) is marked expired.
func (s *Service) CheckPlatform(ctx context.Context, id string) (PlatformStatus, error) {
	reg, _, err := s.setup()
	if err != nil {
		return PlatformStatus{}, err
	}
	p, err := reg.Get(id)
	if err != nil {
		return PlatformStatus{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	sess, seed, err := s.PlatformSession(ctx, p.ID)
	state, account := store.SessionUnknown, ""
	switch {
	case errors.Is(err, vault.ErrDecrypt):
		state = store.SessionExpired
	case err != nil:
		return PlatformStatus{}, err
	default:
		if res, perr := s.plat.deps.probe(ctx, p, sess, seed); perr == nil {
			state, account = res.State, res.Account
		}
	}
	if err := s.st.SetSessionStatus(ctx, p.ID, state, account, s.now()); err != nil {
		return PlatformStatus{}, notConnected(err, p)
	}
	return s.platformStatus(ctx, p.ID)
}

// PlatformSession decrypts a platform's session for code that acts on the
// user's account (probes now; sync and apply later). Never expose the result.
func (s *Service) PlatformSession(ctx context.Context, id string) (browser.Session, string, error) {
	reg, v, err := s.setup()
	if err != nil {
		return browser.Session{}, "", err
	}
	p, err := reg.Get(id)
	if err != nil {
		return browser.Session{}, "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	ps, err := s.st.GetSession(ctx, p.ID)
	if err != nil {
		return browser.Session{}, "", notConnected(err, p)
	}
	plain, err := v.Open(p.ID, ps.Enc)
	if err != nil {
		return browser.Session{}, "", err
	}
	var sess browser.Session
	if err := json.Unmarshal(plain, &sess); err != nil {
		return browser.Session{}, "", vault.ErrDecrypt
	}
	return sess, ps.FPSeed, nil
}

// KeySource reports where the session key comes from (for doctor).
func (s *Service) KeySource() (string, error) {
	_, v, err := s.setup()
	if err != nil {
		return "", err
	}
	return v.Source, nil
}

func (s *Service) platformStatus(ctx context.Context, id string) (PlatformStatus, error) {
	all, err := s.Platforms(ctx)
	if err != nil {
		return PlatformStatus{}, err
	}
	for _, st := range all {
		if st.ID == id {
			return st, nil
		}
	}
	return PlatformStatus{}, fmt.Errorf("%w: unknown platform %q", ErrInvalid, id)
}

// notConnectedError reads "TopCV isn't connected — …" but still counts as
// ErrNotFound, so faces map it to 404 / a not-found tool error.
type notConnectedError struct{ p platform.Platform }

func (e notConnectedError) Error() string {
	return fmt.Sprintf("%s isn't connected — run: braveduck login %s", e.p.Name, e.p.ID)
}

func (e notConnectedError) Is(target error) bool { return target == ErrNotFound }

func notConnected(err error, p platform.Platform) error {
	if store.IsNotFound(err) {
		return notConnectedError{p}
	}
	return err
}

func (s *Service) claimLogin(id string) bool {
	s.plat.mu.Lock()
	defer s.plat.mu.Unlock()
	if s.plat.loading[id] {
		return false
	}
	s.plat.loading[id] = true
	return true
}

func (s *Service) releaseLogin(id string) {
	s.plat.mu.Lock()
	defer s.plat.mu.Unlock()
	delete(s.plat.loading, id)
}

func (s *Service) loginOpen(id string) bool {
	if s.plat == nil {
		return false
	}
	s.plat.mu.Lock()
	defer s.plat.mu.Unlock()
	return s.plat.loading[id]
}
