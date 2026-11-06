package browser

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

// Service is the browser service a shared server runs in a container of its
// own (`braveduck browser-service`): go-rod's launcher.Manager, as in
// rod-manager, with two rules of its own. Every request must carry the key
// (?key=…), so a page open in one of its browsers can't start more of them;
// and the browser binary, profile and working directory are the service's,
// whatever the client asks for.
func Service(bin, key string) http.Handler {
	m := launcher.NewManager()
	m.Defaults = func(http.ResponseWriter, *http.Request) *launcher.Launcher { return launcher.New().Bin(bin) }
	m.BeforeLaunch = func(l *launcher.Launcher, _ http.ResponseWriter, _ *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		l.Bin(bin).Headless(true).
			UserDataDir(filepath.Join(os.TempDir(), "braveduck-browser", hex.EncodeToString(b))).
			Delete(flags.KeepUserDataDir).
			Delete(flags.WorkingDir)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(key)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		m.ServeHTTP(w, r)
	})
}

// BrowserKeyFileEnv names the file holding the browser service's key.
const BrowserKeyFileEnv = "BRAVEDUCK_BROWSER_KEY_FILE"

// serviceURL is the browser service's address with its key added.
func serviceURL(svc string) (string, error) {
	path := strings.TrimSpace(os.Getenv(BrowserKeyFileEnv))
	if path == "" {
		return svc, nil
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(svc)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("key", strings.TrimSpace(string(key)))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// connectService performs the launcher's HTTP settings exchange with the same
// context as the WebSocket connection. launcher.NewManaged uses an unbounded
// http.Get for this first step and does not check the response status.
func connectService(ctx context.Context, addr, seed string) (*cdp.Client, func(), error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, nil, err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, nil, fmt.Errorf("browser service refused its key: HTTP %d %s", res.StatusCode, http.StatusText(res.StatusCode))
	}
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("browser service returned HTTP %d %s", res.StatusCode, http.StatusText(res.StatusCode))
	}
	var l launcher.Launcher
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil {
		return nil, nil, fmt.Errorf("read browser service settings: %w", err)
	}
	if l.Flags == nil {
		return nil, nil, errors.New("browser service returned no launch settings")
	}
	withFlags(l.Headless(true).Leakless(true), seed)
	header := http.Header{}
	header.Set(launcher.HeaderName, string(l.JSON()))
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	// go-rod's WebSocket upgrade reads directly from the connection, so the
	// request context alone cannot interrupt it. Keep the connection tied to
	// ctx after the upgrade too: disconnecting lets the manager stop Chromium.
	var dialer cdp.Dialer = &net.Dialer{}
	if u.Scheme == "wss" {
		dialer = &tls.Dialer{}
		if u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), "443")
		}
	}
	disconnect := func() {}
	ws := &cdp.WebSocket{Dialer: serviceDialFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		disconnect = func() { stop(); _ = conn.Close() }
		return conn, nil
	})}
	if err := ws.Connect(ctx, u.String(), header); err != nil {
		disconnect()
		return nil, nil, err
	}
	return cdp.New().Start(ws), disconnect, nil
}

type serviceDialFunc func(context.Context, string, string) (net.Conn, error)

func (f serviceDialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}
