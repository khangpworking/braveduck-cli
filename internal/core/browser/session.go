package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/core/platform"
)

// Session is a captured login: the platform's cookies and localStorage, and
// nothing else. It is sealed by the vault before it touches disk and must
// never be logged or returned over an API.
type Session struct {
	Cookies    []*proto.NetworkCookie       `json:"cookies"`
	Storage    map[string]map[string]string `json:"storage"` // origin → localStorage
	CapturedAt time.Time                    `json:"captured_at"`
	// Account is a masked label for the logged-in account ("ng…@gmail.com"),
	// safe to show; "" when the platform has no AccountJS.
	Account string `json:"account,omitempty"`
}

// ExpiresHint is when the platform's login cookies (p.AuthCookies) expire —
// the earliest of them — as a "log in again around" date. "" when the
// platform names none or they're session cookies.
func (s Session) ExpiresHint(p platform.Platform) string {
	var min float64
	for _, c := range s.Cookies {
		if c.Session || c.Expires <= 0 || !contains(p.AuthCookies, c.Name) {
			continue
		}
		if e := float64(c.Expires); min == 0 || e < min {
			min = e
		}
	}
	if min == 0 {
		return ""
	}
	return time.Unix(int64(min), 0).UTC().Format(time.RFC3339)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// MaskAccount shortens an account label so it identifies without exposing:
// "nguyen.van.a@gmail.com" → "ng…@gmail.com", "Nguyen Van A" → "Ng…".
func MaskAccount(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	local, domain, isEmail := strings.Cut(s, "@")
	r := []rune(local)
	if len(r) > 2 {
		r = r[:2]
	}
	if isEmail {
		return string(r) + "…@" + domain
	}
	return string(r) + "…"
}

// Probe results.
const (
	LoggedIn  = "connected"
	LoggedOut = "expired"
	Unknown   = "unknown"
)

// ErrLoginTimeout is returned when the user doesn't finish logging in.
var ErrLoginTimeout = errors.New("login window timed out")

// ErrNotLoggedIn is returned when the window closed without a login.
var ErrNotLoggedIn = errors.New("the window closed before you were logged in")

// LoginOptions tunes Login. The zero value is the real thing: a visible window
// the user logs in to by hand.
type LoginOptions struct {
	Timeout time.Duration // default 10 minutes
	// Headless and OnOpen exist for tests: OnOpen gets the login page and
	// plays the user's part.
	Headless bool
	OnOpen   func(*rod.Page)
	// OnDetected is called once when a login is detected, before the window
	// closes (the CLI prints "✓ logged in").
	OnDetected func()
}

// Login opens p's login page in a browser with fingerprint seed, lets the user
// log in by hand (password, OTP, captcha — braveduck reads no form fields and
// injects nothing into the page), and returns the captured session.
//
// It finishes as soon as a login is detected on the page the user is on, or
// when the user closes the window, using the last snapshot taken while it was
// open. Cookies and storage are captured only for p's own domains.
func Login(ctx context.Context, p platform.Platform, seed string, o LoginOptions) (Session, error) {
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Minute
	}
	if !o.Headless && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && !hasDesktop() {
		return Session{}, errors.New("logging in needs a desktop to show the browser window (no DISPLAY found)")
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	profile, err := os.MkdirTemp("", "braveduck-login-*")
	if err != nil {
		return Session{}, err
	}
	defer os.RemoveAll(profile)
	if err := os.Chmod(profile, 0o700); err != nil {
		return Session{}, err
	}

	b, cleanup, err := launch(ctx, launchOpts{Headless: o.Headless, Seed: seed, UserDataDir: profile})
	if err != nil {
		return Session{}, err
	}
	defer cleanup()

	page, err := b.Page(proto.TargetCreateTarget{URL: p.LoginURL})
	if err != nil {
		return Session{}, err
	}
	if o.OnOpen != nil {
		go o.OnOpen(page)
	}
	if !o.Headless {
		go raiseOnWSL(ctx) // WSLg windows otherwise open behind the user's apps
	}

	loginRE, err := regexp.Compile(p.LoginPattern)
	if err != nil {
		return Session{}, err
	}
	snap := Session{Storage: map[string]map[string]string{}}
	tick := time.NewTicker(1500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Session{}, ErrLoginTimeout
			}
			return Session{}, ctx.Err()
		case <-tick.C:
		}

		pages, err := b.Pages()
		if err != nil || len(pages) == 0 {
			// The user closed the window: use what we last saw.
			if len(snap.Cookies) == 0 {
				return Session{}, ErrNotLoggedIn
			}
			snap.CapturedAt = time.Now()
			return snap, nil
		}

		if cs, err := (proto.StorageGetCookies{}).Call(b); err == nil {
			snap.Cookies = ownCookies(p, cs.Cookies)
		}
		state := Unknown
		for _, pg := range pages {
			info, err := pg.Info()
			if err != nil {
				continue
			}
			u, err := url.Parse(info.URL)
			if err != nil || !p.Owns(u.Hostname()) {
				continue
			}
			if st, err := localStorage(pg); err == nil && len(st) > 0 {
				snap.Storage[u.Scheme+"://"+u.Host] = st
			}
			if s := judge(pg, info.URL, p, loginRE); s == LoggedIn {
				state = LoggedIn
			}
		}
		if state == LoggedIn && len(snap.Cookies) > 0 {
			if p.AccountJS != "" {
				for _, pg := range pages {
					if acct := evalString(pg, p.AccountJS); acct != "" {
						snap.Account = MaskAccount(acct)
						break
					}
				}
			}
			if o.OnDetected != nil {
				o.OnDetected()
			}
			snap.CapturedAt = time.Now()
			return snap, nil
		}
	}
}

// hasDesktop reports a GUI on platforms that don't use DISPLAY.
func hasDesktop() bool { return runtime.GOOS == "darwin" || runtime.GOOS == "windows" }

// judge decides whether the page the user is on shows a login. Bounced to
// login → out. With ProbeJS → its verdict. Without → only a page that isn't
// the login page AND isn't the starting login URL counts, so merely opening
// the login form isn't mistaken for success.
func judge(pg *rod.Page, pageURL string, p platform.Platform, loginRE *regexp.Regexp) string {
	if loginRE.MatchString(pageURL) {
		return LoggedOut
	}
	if p.ProbeJS != "" {
		return evalProbe(pg, p.ProbeJS)
	}
	if pageURL == p.LoginURL {
		return Unknown
	}
	return LoggedIn
}

// evalProbe runs a platform's ProbeJS: true → logged in, false → out, else unknown.
func evalProbe(pg *rod.Page, js string) string {
	res, err := pg.Timeout(5 * time.Second).Eval(`() => ` + js)
	if err != nil {
		return Unknown
	}
	switch res.Value.String() {
	case "true":
		return LoggedIn
	case "false":
		return LoggedOut
	}
	return Unknown
}

// evalString runs js in the page and returns its string result, or "".
func evalString(pg *rod.Page, js string) string {
	res, err := pg.Timeout(3 * time.Second).Eval(`() => ` + js)
	if err != nil || res.Value.Nil() {
		return ""
	}
	return res.Value.Str()
}

// waitOutChallenge gives a Cloudflare "Just a moment…" interstitial up to 20s
// to clear on its own (topcv-api-analysis: challenges are intermittent and
// pass on retry). It never tries to defeat one; a page still on the
// challenge afterwards is judged unknown, not logged out.
func waitOutChallenge(pg *rod.Page) bool {
	deadline := time.Now().Add(20 * time.Second)
	for {
		info, err := pg.Info()
		if err != nil || !strings.Contains(info.Title, "Just a moment") {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

func localStorage(pg *rod.Page) (map[string]string, error) {
	res, err := pg.Timeout(3 * time.Second).Eval(`() => JSON.stringify(Object.assign({}, window.localStorage))`)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	err = json.Unmarshal([]byte(res.Value.String()), &m)
	return m, err
}

// ownCookies keeps only the platform's cookies — never another site's.
func ownCookies(p platform.Platform, all []*proto.NetworkCookie) []*proto.NetworkCookie {
	var out []*proto.NetworkCookie
	for _, c := range all {
		if p.Owns(c.Domain) {
			out = append(out, c)
		}
	}
	return out
}

// Open launches a headless browser with s restored (same fingerprint seed as
// at login), for anything that acts on the user's account: probes now; status
// sync and apply later. Call the returned cleanup when done.
func Open(ctx context.Context, p platform.Platform, s Session, seed string) (*rod.Browser, func(), error) {
	b, cleanup, err := launch(ctx, launchOpts{Headless: true, Seed: seed})
	if err != nil {
		return nil, nil, err
	}
	if err := (proto.StorageSetCookies{Cookies: proto.CookiesToParams(ownCookies(p, s.Cookies))}).Call(b); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("restore cookies: %w", err)
	}
	return b, cleanup, nil
}

// restoreStorageJS returns a script that, run before any page script, puts the
// saved localStorage back for the origin it runs on.
func restoreStorageJS(s Session) (string, error) {
	b, err := json.Marshal(s.Storage)
	if err != nil {
		return "", err
	}
	return `(() => { const all = ` + string(b) + `; const mine = all[location.origin];
	  if (!mine) return; for (const k in mine) { try { localStorage.setItem(k, mine[k]) } catch (e) {} } })()`, nil
}

// NewPage opens a page in b with the session's localStorage restored on its
// platform origins.
func NewPage(b *rod.Browser, s Session) (*rod.Page, error) {
	pg, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, err
	}
	js, err := restoreStorageJS(s)
	if err != nil {
		return nil, err
	}
	if _, err := (proto.PageAddScriptToEvaluateOnNewDocument{Source: js}).Call(pg); err != nil {
		return nil, err
	}
	return pg, nil
}

// ProbeResult is a probe's verdict and, when logged in and the platform has an
// AccountJS, the masked account label read from the probe page.
type ProbeResult struct {
	State   string
	Account string
}

// Probe checks whether s is still logged in on p: it opens p.ProbeURL with the
// session and judges the page it lands on. The probe page is an account page,
// so it is also where the account label is reliably present — the page a
// login finishes on often doesn't carry it.
func Probe(ctx context.Context, p platform.Platform, s Session, seed string) (ProbeResult, error) {
	state, pg, cleanup, err := probe(ctx, p, s, seed)
	if cleanup != nil {
		defer cleanup()
	}
	r := ProbeResult{State: state}
	if err == nil && state == LoggedIn && p.AccountJS != "" {
		r.Account = MaskAccount(evalString(pg, p.AccountJS))
	}
	return r, err
}

func probe(ctx context.Context, p platform.Platform, s Session, seed string) (string, *rod.Page, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	b, closeBrowser, err := Open(ctx, p, s, seed)
	if err != nil {
		cancel()
		return Unknown, nil, nil, err
	}
	cleanup := func() { closeBrowser(); cancel() }
	pg, err := NewPage(b, s)
	if err != nil {
		return Unknown, nil, cleanup, err
	}
	pg = pg.Context(ctx)
	if err := pg.Navigate(p.ProbeURL); err != nil {
		return Unknown, pg, cleanup, err
	}
	_ = pg.Timeout(20 * time.Second).WaitLoad()
	if !waitOutChallenge(pg) {
		return Unknown, pg, cleanup, nil
	}
	info, err := pg.Info()
	if err != nil {
		return Unknown, pg, cleanup, err
	}
	loginRE, err := regexp.Compile(p.LoginPattern)
	if err != nil {
		return Unknown, pg, cleanup, err
	}
	if loginRE.MatchString(info.URL) {
		return LoggedOut, pg, cleanup, nil
	}
	if p.ProbeJS != "" {
		return evalProbe(pg, p.ProbeJS), pg, cleanup, nil
	}
	return LoggedIn, pg, cleanup, nil
}

// ErrWindowTimeout means the user left a visible window open past its limit.
var ErrWindowTimeout = errors.New("the window was left open too long")

// ShowPage opens a VISIBLE browser window on url with session s restored (same
// fingerprint seed) and waits until the user closes it, or the timeout. The
// user does whatever they need on the page themselves; braveduck touches
// nothing on it. Used for assisted apply.
func ShowPage(ctx context.Context, p platform.Platform, s Session, seed, url string, timeout time.Duration) error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && !hasDesktop() {
		return errors.New("this needs a desktop to show the browser window (no DISPLAY found)")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	b, cleanup, err := launch(ctx, launchOpts{Headless: false, Seed: seed})
	if err != nil {
		return err
	}
	defer cleanup()
	if err := (proto.StorageSetCookies{Cookies: proto.CookiesToParams(ownCookies(p, s.Cookies))}).Call(b); err != nil {
		return fmt.Errorf("restore cookies: %w", err)
	}
	pg, err := NewPage(b, s)
	if err != nil {
		return err
	}
	if err := pg.Navigate(url); err != nil {
		return err
	}
	go raiseOnWSL(ctx)
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ErrWindowTimeout
			}
			return ctx.Err()
		case <-time.After(time.Second):
		}
		if pages, err := b.Pages(); err != nil || len(pages) == 0 {
			return nil // the user closed the window
		}
	}
}
