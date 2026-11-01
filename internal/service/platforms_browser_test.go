//go:build browser

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/platform"
	"github.com/khangpworking/braveduck-cli/internal/core/vault"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The whole chain with a real browser: a site added through platforms.json →
// Connect (login window, played by the test) → sealed in the DB → check →
// server-side expiry noticed → disconnect.
func TestConnectEndToEndWithRealBrowser(t *testing.T) {
	t.Setenv(vault.PassphraseEnv, "")
	var revoked atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /signin", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<form method="post" action="/signin"><input name="u"><input name="p" type="password"><button id="go">Go</button></form>`))
	})
	mux.HandleFunc("POST /signin", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("u") == "me" && r.FormValue("p") == "pw" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "SECRET-SID", Path: "/", HttpOnly: true, Expires: time.Now().Add(time.Hour)})
			revoked.Store(false)
			http.Redirect(w, r, "/me", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "SECRET-SID" || revoked.Load() {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<p>hi</p>`))
	})
	site := httptest.NewServer(mux)
	defer site.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "platforms.json"), []byte(fmt.Sprintf(`[{"id":"localjobs","name":"Local Jobs",
	  "login_url":"%[1]s/signin","domains":["127.0.0.1"],"probe_url":"%[1]s/me","login_pattern":"/signin"}]`, site.URL)), 0o600)

	st, err := store.Open(filepath.Join(dir, "bd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	s.SetDataDir(dir)
	s.plat.deps.login = func(ctx context.Context, p platform.Platform, seed string, o browser.LoginOptions) (browser.Session, error) {
		o.Headless = true
		o.Timeout = time.Minute
		o.OnOpen = func(pg *rod.Page) {
			pg.MustWaitLoad()
			pg.MustElement(`input[name=u]`).MustInput("me")
			pg.MustElement(`input[name=p]`).MustInput("pw")
			pg.MustElement(`#go`).MustClick()
		}
		return browser.Login(ctx, p, seed, o)
	}
	ctx := context.Background()

	ps, err := s.Connect(ctx, "localjobs", LoginHooks{})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if ps.Status != store.SessionConnected || ps.Builtin {
		t.Fatalf("after connect = %+v", ps)
	}
	raw, _ := st.GetSession(ctx, "localjobs")
	if strings.Contains(string(raw.Enc), "SECRET-SID") {
		t.Fatal("cookie stored in plaintext")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "session.key")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatal("session key missing or not 0600")
	}

	if ps, _ := s.CheckPlatform(ctx, "localjobs"); ps.Status != store.SessionConnected {
		t.Fatalf("check = %+v", ps)
	}
	revoked.Store(true)
	if ps, _ := s.CheckPlatform(ctx, "localjobs"); ps.Status != store.SessionExpired {
		t.Fatalf("check after server-side logout = %+v", ps)
	}
	if del, err := s.Disconnect(ctx, "localjobs"); !del || err != nil {
		t.Fatalf("disconnect = %v %v", del, err)
	}
}
