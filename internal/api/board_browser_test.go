//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// launchBrowser starts headless Chromium (CLOAKBROWSER_BINARY_PATH if set).
func launchBrowser(t *testing.T) *rod.Browser {
	t.Helper()
	l := launcher.New().Headless(true).Set("no-sandbox")
	if bin := os.Getenv("CLOAKBROWSER_BINARY_PATH"); bin != "" {
		l = l.Bin(bin)
	}
	b := rod.New().ControlURL(l.MustLaunch()).MustConnect()
	t.Cleanup(func() { b.MustClose() })
	return b
}

// The board's core promises: hostile job text can't run script, and moves,
// notes and pins reach the server (they once only touched localStorage), with
// no JS errors along the way.
func TestBoardSavesToServerSafely(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	svc.AddJob(ctx, service.NewJob{Title: `<img src=x onerror="window.__pwned=1">Evil`, Company: "Acme", URL: "javascript:window.__pwned=2"})
	svc.AddJob(ctx, service.NewJob{Title: "Data Analyst", Company: "Duck Co"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	var jsErrors []string
	go page.EachEvent(func(e *proto.RuntimeExceptionThrown) {
		jsErrors = append(jsErrors, e.ExceptionDetails.Exception.Description)
	})()
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")

	if n := page.MustEval(`() => document.querySelectorAll(".card").length`).Int(); n != 2 {
		t.Fatalf("%d cards, want 2", n)
	}
	if !page.MustEval(`() => window.__pwned === undefined && !document.querySelector(".card img")`).Bool() {
		t.Fatal("hostile job title executed or injected markup")
	}
	if href := page.MustEval(`() => [...document.querySelectorAll(".card-title")].map(a => a.getAttribute("href")).join(" ")`).Str(); strings.Contains(href, "javascript:") {
		t.Fatalf("javascript: URL kept: %s", href)
	}

	page.MustEval(`() => { const s = document.querySelector('.card[data-id="2"] .move-select'); s.value = "Applied"; s.dispatchEvent(new Event("change")); }`)
	page.MustEval(`() => togglePin("1")`)
	page.MustEval(`() => toggleCardDetail("2")`)
	page.MustElement(`#detail-2 textarea`)
	page.MustEval(`() => { const t = document.querySelector('#detail-2 textarea'); t.value = "call back Monday"; t.dispatchEvent(new Event("input")); }`)

	deadline := time.Now().Add(5 * time.Second)
	for {
		j2, _ := svc.GetJob(ctx, 2)
		j1, _ := svc.GetJob(ctx, 1)
		if j2.Status == "applied" && j2.Notes == "call back Monday" && j1.Pinned {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not saved to the server: job2=%q/%q job1 pinned=%v", j2.Status, j2.Notes, j1.Pinned)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(jsErrors) > 0 {
		t.Fatalf("JS errors: %v", jsErrors)
	}
}

// The board's logins panel: lists every site with its status, shows a saved
// login, and Disconnect deletes it.
func TestLoginsPanel(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	st.SaveSession(ctx, store.PlatformSession{Platform: "topcv", Enc: []byte{1}, CreatedAt: "2026-11-01T00:00:00Z",
		FPSeed: "1", Status: store.SessionConnected})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement("#platformsBtn").MustClick()
	page.MustElement("#platformsList strong")

	text := page.MustElement("#platformsList").MustText()
	for _, want := range []string{"TopCV", "Logged in ✓", "VietnamWorks", "JobOKO", "CareerLink", "Not connected"} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel missing %q:\n%s", want, text)
		}
	}

	// Disconnect asks for confirmation, then deletes.
	wait, handle := page.MustHandleDialog()
	go func() { wait(); handle(true, "") }()
	page.MustElementR("button", "Disconnect").MustClick()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := st.GetSession(ctx, "topcv"); store.IsNotFound(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Disconnect didn't delete the session")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The rating window edits free-text rater ids (no hard-coded model list),
// saves them, and shows the server's reason when a config is invalid.
func TestRatingSettings(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	ctx := context.Background()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement("#ratingBtn").MustClick()
	page.MustElement("#ratingModalOverlay.open") // opens once the stored config has loaded
	if v := page.MustElement("#defaultModelInput").MustProperty("value").Str(); v == "" {
		t.Fatal("default rater not pre-filled from the stored config")
	}
	if strings.Contains(page.MustElement("#ratingModalOverlay").MustText(), "GPT-4o") {
		t.Fatal("hard-coded model list still shown")
	}

	page.MustElement("#defaultModelInput").MustSelectAllText().MustInput("my-rater")
	page.MustElement("#challengerModelsInput").MustInput("rival-a, rival-b")
	page.MustElement("#abModeCheckbox").MustClick()
	page.MustElement("#saveRatingConfigBtn").MustClick()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, _ := svc.RatingConfig(ctx)
		if c.DefaultModel == "my-rater" && c.ABEnabled && len(c.Models) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("config not saved: %+v", c)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Too many raters: the server refuses and the window says why.
	page.MustElement("#ratingBtn").MustClick()
	page.MustElement("#ratingModalOverlay.open")
	page.MustElement("#challengerModelsInput").MustSelectAllText().MustInput("a, b, c")
	page.MustElement("#saveRatingConfigBtn").MustClick()
	msg := page.MustElement("#ratingConfigError").MustWaitVisible().MustText()
	if !strings.Contains(msg, "at most 3") {
		t.Fatalf("error shown = %q", msg)
	}
}
