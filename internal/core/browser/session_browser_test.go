//go:build browser

// Real-browser tests against a fake local job site. They need a Chromium:
//
//	CLOAKBROWSER_BINARY_PATH=/path/to/chrome go test -tags browser ./internal/core/browser/
//
// (plus LD_LIBRARY_PATH for libnss3/libasound on minimal Linux).
package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"

	"github.com/khangpworking/braveduck-cli/internal/core/platform"
)

// fakeSite is a job site with a login form, a members-only page and a token in
// localStorage. Sessions can be revoked server-side to simulate expiry.
type fakeSite struct {
	*httptest.Server
	revoked atomic.Bool
	tracker *httptest.Server // another site that drops a cookie
}

func newFakeSite(t *testing.T) *fakeSite {
	t.Helper()
	fs := &fakeSite{}
	// The tracker is served from "localhost" — a different cookie domain from
	// the job site on 127.0.0.1 — and sets its own cookie. It must not be captured.
	fs.tracker = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "tracker", Value: "third-party", Path: "/"})
		w.Write([]byte("ok"))
	}))
	trackerURL := strings.Replace(fs.tracker.URL, "127.0.0.1", "localhost", 1)

	mux := http.NewServeMux()
	loggedIn := func(r *http.Request) bool {
		c, err := r.Cookie("sid")
		x, xerr := r.Cookie("xsrf")
		return err == nil && c.Value == "valid" && xerr == nil && x.Value == "tok" && !fs.revoked.Load()
	}
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<html><body><script>window.app={auth:{isAuth:false}}</script>
		  <img src="%s/pixel">
		  <form method="post" action="/login"><input name="user"><input name="pass" type="password">
		  <button id="go">Log in</button></form></body></html>`, trackerURL)
	})
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("user") == "demo" && r.FormValue("pass") == "pw" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "valid", Path: "/", HttpOnly: true,
				Expires: time.Now().Add(48 * time.Hour)})
			// Like TopCV's XSRF-TOKEN: a session cookie (no expiry) the site
			// requires alongside the dated one.
			http.SetCookie(w, &http.Cookie{Name: "xsrf", Value: "tok", Path: "/"})
			fs.revoked.Store(false)
			http.Redirect(w, r, "/home", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
	})
	member := func(w http.ResponseWriter, r *http.Request) {
		if !loggedIn(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Write([]byte(`<html><body><script>window.app={auth:{isAuth:true}}; window.me="demo.user@example.com";
		  if (!localStorage.getItem("tok")) localStorage.setItem("tok","abc123")</script>
		  <p id="hello">Welcome back</p></body></html>`))
	}
	mux.HandleFunc("GET /home", member)
	mux.HandleFunc("GET /account", member)
	fs.Server = httptest.NewServer(mux)
	t.Cleanup(func() { fs.Close(); fs.tracker.Close() })
	return fs
}

func (fs *fakeSite) platform(withProbeJS bool) platform.Platform {
	p := platform.Platform{
		ID: "fakejobs", Name: "Fake Jobs",
		LoginURL:     fs.URL + "/login",
		Domains:      []string{"127.0.0.1"},
		ProbeURL:     fs.URL + "/account",
		LoginPattern: `/login`,
		AuthCookies:  []string{"sid"},
		AccountJS:    `(() => window.me || null)()`,
	}
	if withProbeJS {
		p.ProbeJS = `(() => { const a = window.app && window.app.auth; return a ? a.isAuth === true : null })()`
	}
	return p
}

// typeCredentials plays the user: fill the form and submit.
func typeCredentials(user, pass string) func(*rod.Page) {
	return func(pg *rod.Page) {
		pg.MustWaitLoad()
		pg.MustElement(`input[name=user]`).MustInput(user)
		pg.MustElement(`input[name=pass]`).MustInput(pass)
		pg.MustElement(`#go`).MustClick()
	}
}

func TestLoginCaptureProbeExpire(t *testing.T) {
	for _, withJS := range []bool{true, false} {
		t.Run(fmt.Sprintf("probeJS=%v", withJS), func(t *testing.T) {
			fs := newFakeSite(t)
			p := fs.platform(withJS)
			ctx := context.Background()
			seed := NewSeed()

			detected := false
			s, err := Login(ctx, p, seed, LoginOptions{
				Headless: true, Timeout: time.Minute,
				OnOpen:     typeCredentials("demo", "pw"),
				OnDetected: func() { detected = true },
			})
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			if !detected {
				t.Error("login finished without OnDetected")
			}

			var names []string
			for _, c := range s.Cookies {
				names = append(names, c.Name+"@"+c.Domain)
			}
			if !strings.Contains(strings.Join(names, ","), "sid@127.0.0.1") {
				t.Fatalf("session cookie not captured: %v", names)
			}
			for _, c := range s.Cookies {
				if c.Name == "tracker" {
					t.Fatalf("captured another site's cookie: %v", names)
				}
			}
			u, _ := url.Parse(fs.URL)
			if s.Storage["http://"+u.Host]["tok"] != "abc123" {
				t.Fatalf("localStorage not captured: %v", s.Storage)
			}
			if s.ExpiresHint(p) == "" {
				t.Error("no expiry hint from the 48h auth cookie")
			}
			if s.Account != "de…@example.com" {
				t.Errorf("account = %q, want masked de…@example.com", s.Account)
			}

			state, err := Probe(ctx, p, s, seed)
			if err != nil || state != LoggedIn {
				t.Fatalf("probe with fresh session = %q, %v", state, err)
			}

			fs.revoked.Store(true)
			if state, _ := Probe(ctx, p, s, seed); state != LoggedOut {
				t.Fatalf("probe after server-side revoke = %q, want %q", state, LoggedOut)
			}
		})
	}
}

func TestLoginWindowClosedWithoutLogin(t *testing.T) {
	fs := newFakeSite(t)
	_, err := Login(context.Background(), fs.platform(true), NewSeed(), LoginOptions{
		Headless: true, Timeout: time.Minute,
		OnOpen: func(pg *rod.Page) {
			pg.MustWaitLoad()
			time.Sleep(2 * time.Second)
			pg.MustClose() // the user gives up
		},
	})
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestLoginWrongPasswordTimesOut(t *testing.T) {
	fs := newFakeSite(t)
	_, err := Login(context.Background(), fs.platform(true), NewSeed(), LoginOptions{
		Headless: true, Timeout: 8 * time.Second,
		OnOpen: typeCredentials("demo", "wrong"),
	})
	if !errors.Is(err, ErrLoginTimeout) {
		t.Fatalf("err = %v, want ErrLoginTimeout (a failed login must never be saved)", err)
	}
}
