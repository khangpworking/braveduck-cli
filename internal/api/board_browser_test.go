//go:build browser

package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
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
	// Every page op gets a deadline, so a stuck step fails this test with a
	// timeout error instead of hanging the whole suite.
	b := rod.New().ControlURL(l.MustLaunch()).MustConnect().Timeout(60 * time.Second)
	t.Cleanup(func() { b.CancelTimeout().MustClose() })
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
	clickID(page, "platformsBtn")
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
	clickID(page, "ratingBtn")
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
	clickID(page, "saveRatingConfigBtn")
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
	clickID(page, "ratingBtn")
	page.MustElement("#ratingModalOverlay.open")
	page.MustElement("#challengerModelsInput").MustSelectAllText().MustInput("a, b, c")
	clickID(page, "saveRatingConfigBtn")
	msg := page.MustElement("#ratingConfigError").MustWaitVisible().MustText()
	if !strings.Contains(msg, "at most 3") {
		t.Fatalf("error shown = %q", msg)
	}
}

// Keywords saved on the board reach the store (PUT /api/keywords used to 404),
// and the Sources window changes a schedule and runs "Crawl now".
func TestKeywordsAndSources(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	fc := &boardFakeCrawler{}
	svc.SetCrawlers(fc)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	ctx := context.Background()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	clickID(page, "keywordsBtn")
	page.MustElement("#kw-search_queries").MustInput("n8n, ai automation")
	clickID(page, "saveKeywordsBtn")
	waitUntil(t, func() bool {
		k, _ := svc.Keywords(ctx)
		return len(k["search_queries"]) == 2
	}, "search queries not saved")

	clickID(page, "sourcesBtn")
	page.MustElement("#sourcesList select")
	page.MustEval(`() => { const s = [...document.querySelectorAll("#sourcesList select")].pop(); s.value = "360"; s.dispatchEvent(new Event("change")); }`)
	waitUntil(t, func() bool {
		for _, sc := range must(svc.CrawlSchedules(ctx)) {
			if sc.Source == "vietnamworks" {
				return sc.EveryMinutes == 360
			}
		}
		return false
	}, "schedule not saved")

	page.MustEval(`() => [...document.querySelectorAll("#sourcesList button")].pop().click()`)
	waitUntil(t, func() bool {
		l, _ := svc.ListJobs(ctx, service.ListFilter{Source: "vietnamworks"})
		return len(l) == 1
	}, "Crawl now didn't add the job")
}

type boardFakeCrawler struct{}

func (boardFakeCrawler) Name() string { return "vietnamworks" }
func (boardFakeCrawler) Search(context.Context, string) ([]crawl.Listing, error) {
	return []crawl.Listing{{Title: "n8n Engineer", Company: "Duck Co", URL: "https://www.vietnamworks.com/x-1-jv"}}, nil
}

func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func must[T any](v T, _ error) T { return v }

// Edit details on a card persists, and a hostile value round-trips as text
// inside the input (no attribute break-out).
func TestEditDetailsOnBoard(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	svc.AddJob(ctx, service.NewJob{Title: "Lead", Company: `x" onfocus="window.__pwned=1`})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustEval(`() => toggleCardDetail("1")`)
	page.MustElement("#detail-1 details summary").MustClick()
	in := page.MustElement("#edit-company-1")
	if v := in.MustProperty("value").Str(); v != `x" onfocus="window.__pwned=1` {
		t.Fatalf("input value = %q (escaping broken?)", v)
	}
	in.MustFocus()
	if page.MustEval(`() => window.__pwned === 1`).Bool() {
		t.Fatal("attribute injection executed")
	}
	in.MustSelectAllText().MustInput("Việc làm AI Engineer -Việt Nam")
	page.MustElementR("button", "Save details").MustClick()
	waitUntil(t, func() bool {
		j, _ := svc.GetJob(ctx, 1)
		return j.Company == "Việc làm AI Engineer -Việt Nam"
	}, "company not saved")
}

// The CV/email flow on the board: Ask your AI shows the request, the user
// pastes what their AI wrote, the CV downloads as a real PDF, and the draft
// appears for review.
func TestAskAICVAndDraft(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, _ := store.Open(":memory:")
	defer st.Close()
	svc := service.New(st)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	os.WriteFile(filepath.Join(dir, "profile.md"), []byte("Backend engineer, Go and Postgres."), 0o600)
	ctx := context.Background()
	j, _ := svc.AddJob(ctx, service.NewJob{Title: "Go Engineer", Company: "Duck Co"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustEval(`() => toggleCardDetail("1")`)
	page.MustElementR("button", "Ask your AI to tailor it").MustClick()
	page.MustElement("#askModalOverlay.open") // the request is fetched before the window opens
	if p := page.MustElement("#askPrompt").MustText(); !strings.Contains(p, "tailor my CV for job #1") {
		t.Fatalf("prompt = %q", p)
	}
	page.MustElement("#askPaste").MustInput("# Nguyen Van A\n\n## Experience\n- Go and Postgres at Duck Co, five years")
	page.MustElementR("button", "Save what my AI wrote").MustClick()
	waitUntil(t, func() bool { jj, _ := svc.GetJob(ctx, j.ID); return jj.CVVersion == 1 }, "CV not saved")

	// The PDF is made by the real browser.
	resp := do(t, http.MethodGet, fmt.Sprintf("%s/api/jobs/%d/cv.pdf", ts.URL, j.ID), "")
	pdf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.HasPrefix(string(pdf), "%PDF") || len(pdf) < 1000 {
		t.Fatalf("pdf = %d, %d bytes", resp.StatusCode, len(pdf))
	}

	// Draft pasted in the "Subject: …" format lands for review.
	page.MustEval(`() => askAI("draft", "1")`)
	page.MustElement("#askModalOverlay.open")
	page.MustElement("#askPaste").MustInput("Subject: Go Engineer application\n\nHello, I'd like to apply; my CV is attached.")
	page.MustElementR("button", "Save what my AI wrote").MustClick()
	waitUntil(t, func() bool {
		jj, _ := svc.GetJob(ctx, j.ID)
		return strings.Contains(jj.DraftEmail, "Go Engineer application")
	}, "draft not saved")
	page.MustEval(`() => { AppState.expandedCards.add("1"); refreshAppState(); }`)
	if v := page.MustElement("#draftSubject-1").MustProperty("value").Str(); v != "Go Engineer application" {
		t.Fatalf("draft subject on the card = %q", v)
	}
}

// clickID clicks a static control by id with a DOM click. rod's MustClick
// waits for the element to be stable and uncovered, which can hang while the
// board re-renders right after load; a DOM click is what a user's click
// delivers anyway.
func clickID(page *rod.Page, id string) {
	page.MustElement("#" + id)
	page.MustEval(`id => document.getElementById(id).click()`, id)
}
