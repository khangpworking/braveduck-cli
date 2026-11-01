package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// A cross-site "simple" POST (no custom headers, so no CORS preflight) must not
// reach the API — this is exactly what a malicious page could send.
func TestAPIRequiresToken(t *testing.T) {
	ts, st := newTestServer(t)
	for _, auth := range []string{"", "Bearer wrong", testToken} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/jobs",
			strings.NewReader(`{"content":"Job pasted by another site"}`))
		req.Header.Set("Content-Type", "text/plain")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("auth %q: status %d, want 401", auth, resp.StatusCode)
		}
	}
	if list, _ := st.List(context.Background(), storeFilterAll); len(list) != 0 {
		t.Fatalf("unauthenticated POST created %d job(s)", len(list))
	}
}

// DNS rebinding: the attacker's page is same-origin with us, but its Host header
// names the attacker's domain. Even with a valid token, refuse it.
func TestRejectsNonLoopbackHost(t *testing.T) {
	ts, _ := newTestServer(t)
	for _, path := range []string{"/", "/api/jobs"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Host = "evil.example:8080"
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s with foreign Host: status %d, want 403", path, resp.StatusCode)
		}
	}
	for _, h := range []string{"localhost:8080", "127.0.0.1", "[::1]:9000", "LOCALHOST"} {
		if !isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = false", h)
		}
	}
}

func TestIndexCarriesTokenAndIsntCached(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `<meta name="braveduck-token" content="`+testToken+`">`) {
		t.Fatal("index.html doesn't carry the token")
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", resp.Header.Get("Cache-Control"))
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("board can be framed (no X-Frame-Options)")
	}
}

func TestPinAndKeepToggle(t *testing.T) {
	ts, st := newTestServer(t)
	if _, err := st.Add(context.Background(), jobs.Job{Title: "X"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		path         string
		pinned, keep bool
	}{
		{"/pin", true, false},
		{"/keep", true, true},
		{"/pin", false, true},
	} {
		resp := do(t, http.MethodPost, ts.URL+"/api/jobs/1"+step.path, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %s = %d", step.path, resp.StatusCode)
		}
		var j jobs.Job
		if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
			t.Fatal(err)
		}
		if j.Pinned != step.pinned || j.Keep != step.keep {
			t.Fatalf("after %s: pinned=%v keep=%v, want %v/%v", step.path, j.Pinned, j.Keep, step.pinned, step.keep)
		}
	}
	if resp := do(t, http.MethodPost, ts.URL+"/api/jobs/99/pin", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pin missing job = %d, want 404", resp.StatusCode)
	}
}

func TestRatingConfigRoundTrip(t *testing.T) {
	ts, _ := newTestServer(t)

	var got ratingConfigJSON
	getJSON(t, ts.URL+"/api/rating-config", &got)
	if got.DefaultModel == "" || len(got.Models) != 1 {
		t.Fatalf("fresh config = %+v, want the single default rater", got)
	}

	resp := do(t, http.MethodPut, ts.URL+"/api/rating-config",
		`{"default_model":"b","ab_mode":true,"models":["a","b"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d", resp.StatusCode)
	}
	getJSON(t, ts.URL+"/api/rating-config", &got)
	if got.DefaultModel != "b" || !got.ABMode || len(got.Models) != 2 {
		t.Fatalf("config not persisted: %+v", got)
	}

	resp = do(t, http.MethodPut, ts.URL+"/api/rating-config",
		`{"default_model":"zzz","ab_mode":false,"models":["a"]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid PUT = %d, want 400", resp.StatusCode)
	}
}

func TestCreateRejectsInternalURL(t *testing.T) {
	ts, st := newTestServer(t)
	resp := do(t, http.MethodPost, ts.URL+"/api/jobs", `{"content":"http://127.0.0.1:6379/"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST localhost URL = %d, want 400", resp.StatusCode)
	}
	if list, _ := st.List(context.Background(), storeFilterAll); len(list) != 0 {
		t.Fatal("internal URL was stored")
	}
}

func TestPlatformsAPI(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	ts, _ := newTestServer(t)
	var ps []map[string]any
	getJSON(t, ts.URL+"/api/platforms", &ps)
	if len(ps) != 4 || ps[0]["status"] != "not_connected" {
		t.Fatalf("platforms = %v", ps)
	}
	if resp := do(t, http.MethodPost, ts.URL+"/api/platforms/nope/login", ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown platform = %d, want 400", resp.StatusCode)
	}
	if resp := do(t, http.MethodDelete, ts.URL+"/api/platforms/topcv/login", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("disconnect = %d", resp.StatusCode)
	}
	// Login windows can only be opened by the token-holding board.
	resp, err := http.Post(ts.URL+"/api/platforms/topcv/login", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tokenless connect = %d, want 401", resp.StatusCode)
	}
}
