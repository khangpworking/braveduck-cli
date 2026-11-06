package browser

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

// launchOpts configures one browser launch.
type launchOpts struct {
	Headless bool
	// Seed pins CloakBrowser's fingerprint. Empty = a fresh random one, which
	// is right for anonymous fetching; a logged-in account must always present
	// the same seed, or it looks like a stolen session.
	Seed        string
	UserDataDir string // empty = launcher default (a fresh temp profile)
}

// NewSeed returns a fingerprint seed in CloakBrowser's range.
func NewSeed() string { return strconv.Itoa(10000 + rand.Intn(90000)) }

// slots caps how many browsers run at once; nil means no cap.
var slots chan struct{}

// SetMaxBrowsers caps the browsers running at once across the process (a
// shared server runs every user's crawls and fetches); 0 removes the cap.
// Call it at start-up, before any launch.
func SetMaxBrowsers(n int) {
	slots = nil
	if n > 0 {
		slots = make(chan struct{}, n)
	}
}

type ownerKey struct{}

// WithOwner marks ctx as one user's work. Browsers launched under it count
// against that user: on a shared server each user runs at most one browser
// at a time, so nobody can hold every slot.
func WithOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}

// ownerTurns holds each owner's single browser turn (capacity-1 channels).
var ownerTurns sync.Map

// acquire waits for the owner's turn (if ctx has one) and a free browser
// slot; the returned func gives both back.
func acquire(ctx context.Context) (func(), error) {
	var turn chan struct{}
	if owner, _ := ctx.Value(ownerKey{}).(string); owner != "" {
		v, _ := ownerTurns.LoadOrStore(owner, make(chan struct{}, 1))
		turn = v.(chan struct{})
		select {
		case turn <- struct{}{}:
		case <-ctx.Done():
			return nil, fmt.Errorf("browser: waiting for your other browser to finish: %w", ctx.Err())
		}
	}
	endTurn := func() {
		if turn != nil {
			<-turn
		}
	}
	if slots == nil {
		var once sync.Once
		return func() { once.Do(endTurn) }, nil
	}
	select {
	case slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-slots; endTurn() }) }, nil
	case <-ctx.Done():
		endTurn()
		return nil, fmt.Errorf("browser: waiting for a free browser: %w", ctx.Err())
	}
}

// launch starts the provisioned CloakBrowser Chromium with its stealth flags —
// fingerprint seed, spoofed platform, Vietnam locale/timezone — minus the two
// Chromium defaults that leak automation, and connects to it. The returned
// cleanup closes the browser and removes its temp profile.
func launch(ctx context.Context, o launchOpts) (*rod.Browser, func(), error) {
	release, err := acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	b, cleanup, err := launchBrowser(ctx, o)
	if err != nil {
		release()
		return nil, nil, err
	}
	return b, func() { cleanup(); release() }, nil
}

// BrowserServiceEnv names a go-rod browser service (rod-manager, e.g.
// ws://browser:7317) to run every browser in instead of locally. A shared
// server uses it so the browser, which renders other sites' pages, runs in
// a container of its own: no users' data, no secrets, its own network.
const BrowserServiceEnv = "BRAVEDUCK_BROWSER_SERVICE"

func launchBrowser(ctx context.Context, o launchOpts) (*rod.Browser, func(), error) {
	if svc := strings.TrimSpace(os.Getenv(BrowserServiceEnv)); svc != "" {
		return launchRemote(ctx, svc, o)
	}
	bin, err := EnsureBinary(ctx)
	if err != nil {
		return nil, nil, err
	}
	l := withFlags(launcher.New().Bin(bin).Context(ctx).Headless(o.Headless), o.Seed)
	if o.UserDataDir != "" {
		l = l.UserDataDir(o.UserDataDir)
	}
	if !o.Headless {
		// Without these Chromium picks its own geometry; on WSLg that was
		// half off-screen (-22,-22) and taller than a 1080p display. This
		// fits a 1366x768 laptop screen.
		l = l.Set(flags.Flag("window-position"), "60,40").
			Set(flags.Flag("window-size"), "1024,720")
	}

	controlURL, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("browser: launch: %w%s", err, LibsHint())
	}
	b := rod.New().ControlURL(controlURL).Context(ctx)
	if err := b.Connect(); err != nil {
		l.Cleanup()
		return nil, nil, fmt.Errorf("browser: connect: %w", err)
	}
	return b, func() { _ = b.Close(); l.Cleanup() }, nil
}

// withFlags adds CloakBrowser's flags — fingerprint seed (a fresh one if
// seed is ""), spoofed platform, Vietnam locale/timezone — minus the two
// Chromium defaults that leak automation.
func withFlags(l *launcher.Launcher, seed string) *launcher.Launcher {
	fpPlatform := "windows"
	if runtime.GOOS == "darwin" {
		fpPlatform = "macos"
	}
	if seed == "" {
		seed = NewSeed()
	}
	return l.Set(flags.Flag("no-sandbox")).
		Set(flags.Flag("fingerprint"), seed).
		Set(flags.Flag("fingerprint-platform"), fpPlatform).
		Set(flags.Flag("lang"), "vi-VN").
		Set(flags.Flag("fingerprint-timezone"), "Asia/Ho_Chi_Minh").
		Delete(flags.Flag("enable-automation")).
		Delete(flags.Flag("enable-unsafe-swiftshader"))
}

// launchRemote starts a headless browser in the browser service and
// connects to it; the service kills it when the connection closes.
func launchRemote(ctx context.Context, svc string, o launchOpts) (*rod.Browser, func(), error) {
	if !o.Headless {
		return nil, nil, errors.New("browser: a window can't open in the browser service")
	}
	addr, err := serviceURL(svc)
	if err != nil {
		return nil, nil, fmt.Errorf("browser: the browser service's key: %w", err)
	}
	// The bootstrap changes ws(s) to http(s), so replacing the original URL
	// alone misses keys in connection errors. Hide the key in either spelling,
	// including a key supplied directly in the configured service URL.
	redact := func(err error) error {
		msg := fmt.Sprintf("browser: the browser service at %s: %s", svc, err)
		for _, raw := range []string{svc, addr} {
			if u, err := url.Parse(raw); err == nil {
				// QueryEscape canonicalizes escapes. Also hide the exact value
				// supplied in the URL, which may use other valid spellings.
				for _, pair := range strings.Split(u.RawQuery, "&") {
					name, value, _ := strings.Cut(pair, "=")
					name, _ = url.QueryUnescape(name)
					if name == "key" && value != "" {
						msg = strings.ReplaceAll(msg, value, "[redacted]")
					}
				}
				for _, key := range u.Query()["key"] {
					if key != "" {
						msg = strings.ReplaceAll(msg, url.QueryEscape(key), "[redacted]")
						msg = strings.ReplaceAll(msg, key, "[redacted]")
					}
				}
			}
		}
		return errors.New(msg)
	}
	l, err := launcher.NewManaged(addr)
	if err != nil {
		return nil, nil, redact(err)
	}
	l = withFlags(l.Context(ctx).Headless(true).Leakless(true), o.Seed)
	client, err := l.Client()
	if err != nil {
		return nil, nil, redact(err)
	}
	b := rod.New().Client(client).Context(ctx)
	if err := b.Connect(); err != nil {
		return nil, nil, fmt.Errorf("browser: connect to the browser service: %w", err)
	}
	return b, func() { _ = b.Close() }, nil
}

// LibsHint explains the most common launch failure on minimal Linux (WSL,
// containers): Chromium's NSS/ALSA libraries aren't installed. "" when fine.
func LibsHint() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := os.Stat("/usr/lib/x86_64-linux-gnu/libnss3.so"); err == nil {
		return ""
	}
	if _, err := os.Stat("/usr/lib/aarch64-linux-gnu/libnss3.so"); err == nil {
		return ""
	}
	return " — Chromium needs system libraries; run: sudo apt install -y libnss3 libasound2t64"
}
