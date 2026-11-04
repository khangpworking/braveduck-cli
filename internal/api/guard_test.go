package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestCVAndDraftAPI(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, _ := store.Open(":memory:")
	defer st.Close()
	svc := service.New(st)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	os.WriteFile(filepath.Join(dir, "profile.md"), []byte("Backend engineer, Go."), 0o600)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	ctx := context.Background()
	j, _ := svc.AddJob(ctx, service.NewJob{Title: "Go Engineer"})
	base := fmt.Sprintf("%s/api/jobs/%d", ts.URL, j.ID)

	var ask askOut
	resp := do(t, http.MethodPost, base+"/cv", "")
	json.NewDecoder(resp.Body).Decode(&ask)
	if resp.StatusCode != 200 || !strings.Contains(ask.Prompt, "Using braveduck") || !strings.Contains(ask.Brief, "braveduck cv") {
		t.Fatalf("cv request = %d %+v", resp.StatusCode, ask)
	}
	if resp := do(t, http.MethodGet, base+"/cv.pdf", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pdf before any CV = %d", resp.StatusCode)
	}
	if resp := do(t, http.MethodPut, base+"/cv", `{"markdown":"# A\n\n## Experience\n- Go at Duck Co for five years, backend services"}`); resp.StatusCode != 200 {
		t.Fatalf("put cv = %d", resp.StatusCode)
	}
	orig := renderPDF
	t.Cleanup(func() { renderPDF = orig })
	var gotHTML string
	renderPDF = func(_ context.Context, html string) ([]byte, error) {
		gotHTML = html
		return []byte("%PDF-1.4 fake"), nil
	}
	resp = do(t, http.MethodGet, base+"/cv.pdf", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(body), "%PDF") || !strings.Contains(gotHTML, "<h2>Experience</h2>") {
		t.Fatalf("pdf = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	if resp := do(t, http.MethodPut, base+"/draft", `{"to":"hr@duck.vn","subject":"Application","body":"Hello there, applying for this role."}`); resp.StatusCode != 200 {
		t.Fatalf("put draft = %d", resp.StatusCode)
	}
	if resp := do(t, http.MethodPost, base+"/draft/sent", ""); resp.StatusCode != 200 {
		t.Fatalf("sent = %d", resp.StatusCode)
	}
	if j, _ := svc.GetJob(ctx, j.ID); j.Status != "applied" {
		t.Fatalf("after sent: %q", j.Status)
	}

	// No profile → 409 with the reason, not a 500.
	os.WriteFile(filepath.Join(dir, "profile.md"), []byte(""), 0o600)
	if resp := do(t, http.MethodPost, base+"/rate", ""); resp.StatusCode != http.StatusConflict {
		t.Fatalf("rate with empty profile = %d, want 409", resp.StatusCode)
	}
}

// Apply refuses clearly when it can't run (no window is ever opened here).
func TestApplyRefusals(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	ts, st := newTestServer(t)
	id, _ := st.Add(context.Background(), jobs.Job{Title: "VNW", URL: "https://www.vietnamworks.com/x-1-jv"}, time.Now())
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/jobs/%d/apply", ts.URL, id), "")
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(out["error"], "TopCV jobs") {
		t.Fatalf("non-TopCV apply = %d %v", resp.StatusCode, out)
	}
}
