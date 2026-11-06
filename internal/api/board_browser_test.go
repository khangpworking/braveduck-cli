//go:build browser

package api

import (
	"context"
	"fmt"
	"github.com/khangpworking/braveduck-cli/internal/core/geo"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
	"github.com/khangpworking/braveduck-cli/internal/core/jev"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
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
	// Close, then kill the process and remove its profile: otherwise every
	// test's Chromium keeps running underneath the next test.
	t.Cleanup(func() { _ = b.CancelTimeout().Close(); l.Kill(); l.Cleanup() })
	go b.EachEvent(func(e *proto.PageJavascriptDialogOpening) {
		t.Logf("DIALOG %s: %q", e.Type, e.Message)
	}, func(e *proto.RuntimeExceptionThrown) {
		t.Logf("JS EXCEPTION: %s", e.ExceptionDetails.Exception.Description)
	}, func(e *proto.PageFrameNavigated) {
		t.Logf("NAVIGATED %s", e.Frame.URL)
	}, func(e *proto.TargetTargetCrashed) {
		t.Logf("RENDERER CRASHED")
	})()
	return b
}

// Header and toolbar controls stay reachable without horizontal scrolling.
func TestBoardControlsFitNarrowViewports(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	for _, source := range []string{"facebook", "vietnamworks", "careerlink", "topcv", "paste"} {
		if _, err := svc.AddJob(context.Background(), service.NewJob{Title: source + " engineer", Source: source}); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	for _, width := range []int{320, 390, 768, 1024, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			page.MustSetViewport(width, 844, 1, false)
			page.MustElement(".top-nav").MustWaitStable()
			// Each control and statistic must fit in the viewport, and the
			// button's center must actually receive a pointer click once it is
			// scrolled into view (only vertical scrolling is fine).
			if problem := page.MustEval(`() => {
				const width = document.documentElement.clientWidth;
				for (const el of document.querySelectorAll('.top-nav button, .stat-pill, .toolbar button, .filter-cb')) {
					const r = el.getBoundingClientRect();
					if (r.left < 0 || r.right > width || r.top < 0)
						return el.textContent.trim() + ' outside viewport: ' + JSON.stringify(r);
					if (el.tagName !== 'BUTTON') continue;
					el.scrollIntoView({block: 'center'});
					const c = el.getBoundingClientRect();
					const hit = document.elementFromPoint(c.x + c.width / 2, c.y + c.height / 2);
					window.scrollTo(0, 0);
					if (!el.contains(hit))
						return el.textContent.trim() + ' is covered';
				}
				return '';
			}`).Str(); problem != "" {
				t.Fatal(problem)
			}
		})
	}
}

// Closed overlays must not intercept Tab navigation; opening them makes their
// inputs focusable again. Opacity alone leaves invisible forms in the tab order.
func TestBoardClosedOverlaysDoNotTakeFocus(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".column")

	checkTabOrder := func() {
		t.Helper()
		page.MustEval(`() => document.getElementById('openDrawerBtn').focus()`)
		limit := page.MustEval(`() => document.querySelectorAll('button, input, textarea, select, a[href], [tabindex]').length + 2`).Int()
		for i := 0; i < limit; i++ {
			page.Keyboard.MustType(input.Tab)
			if hidden := page.MustEval(`() => {
				const el = document.activeElement;
				return el.closest('.modal-overlay:not(.open), .drawer:not(.open)') ? el.outerHTML : '';
			}`).Str(); hidden != "" {
				t.Fatalf("Tab focused a closed overlay: %s", hidden)
			}
			if page.MustEval(`() => document.activeElement.id === 'openDrawerBtn'`).Bool() {
				return
			}
		}
		t.Fatal("Tab did not return to the board's Add Job button")
	}
	checkTabOrder()

	clickID(page, "openDrawerBtn")
	page.MustElement("#pasteJDTextarea").MustFocus()
	if !page.MustEval(`() => document.activeElement.id === 'pasteJDTextarea'`).Bool() {
		t.Fatal("open drawer input cannot receive focus")
	}
	clickID(page, "closeDrawerBtn")
	page.MustElement("#closeDrawerBtn").MustWaitInvisible()

	openModal(t, page, "ratingBtn", "ratingModalOverlay")
	page.MustElement("#defaultModelInput").MustFocus()
	if !page.MustEval(`() => document.activeElement.id === 'defaultModelInput'`).Bool() {
		t.Fatal("open modal input cannot receive focus")
	}
	page.MustElement("#ratingModalOverlay .modal-close-btn").MustClick()
	page.MustElement("#ratingModalOverlay .modal-footer .modal-close-btn").MustWaitInvisible()
	checkTabOrder()
}

// Mobile column headers toggle visibility and remember the choice on reload.
func TestBoardMobileColumnsToggle(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	if _, err := svc.AddJob(context.Background(), service.NewJob{Title: "Mobile engineer"}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	setViewportWidth(page, 390)
	waitVisible(page, ".card")
	page.MustElement("#col-Saved .column-header").MustClick()
	if page.MustElement(".card").MustVisible() {
		t.Fatal("mobile header did not collapse its cards")
	}
	page.MustReload().MustWaitLoad()
	if page.MustElement(".card").MustVisible() {
		t.Fatal("collapsed column was expanded after reload")
	}
	page.MustElement("#col-Saved .column-header").MustClick()
	waitVisible(page, ".card")
	// On a desktop the header folds the lane to a strip instead (and a
	// second click unfolds it); the phone accordion's state isn't touched.
	setViewportWidth(page, 1024)
	page.MustElement("#col-Saved .column-header").MustEval(`() => this.click()`)
	if page.MustElement(".card").MustVisible() || !page.MustHas("#col-Saved.folded") {
		t.Fatal("desktop header didn't fold its lane")
	}
	page.MustElement("#col-Saved .column-header").MustEval(`() => this.click()`)
	waitVisible(page, ".card")
	if v := page.MustEval(`() => localStorage.getItem("col_collapsed_Saved")`).Str(); v != "false" {
		t.Fatalf("desktop folding changed the phone accordion state to %q", v)
	}
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
	openModal(t, page, "platformsBtn", "platformsModalOverlay")
	page.MustElement("#platformsList strong")

	text := page.MustElement("#platformsList").MustText()
	for _, want := range []string{"TopCV", "Logged in ✓", "VietnamWorks", "JobOKO", "CareerLink", "Not connected"} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel missing %q:\n%s", want, text)
		}
	}

	// Disconnect asks for confirmation, then deletes.
	wait, handle := page.MustHandleDialog()
	go func() {
		// rod's wait panics once the page's context ends; in a goroutine
		// that would take down the whole test binary, not just this test.
		defer func() { _ = recover() }()
		wait()
		handle(true, "")
	}()
	page.MustElementR("button", "Disconnect").MustEval(`() => this.click()`)
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
	openModal(t, page, "ratingBtn", "ratingModalOverlay")
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
	openModal(t, page, "ratingBtn", "ratingModalOverlay")
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
	ctx := context.Background()
	if _, err := svc.SetCrawlSchedule(ctx, "vietnamworks", 120); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordCrawlRun(ctx, "vietnamworks", true, "", time.Now().Add(-5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	// The store applies the source's schedule to health. The warning must
	// not claim a fixed one-hour window for this two-hourly crawler.
	warning := page.MustElement("#unhealthyContainer .unhealthy").MustText()
	if !strings.Contains(warning, "no recent successful run") || !strings.Contains(warning, "last report: 5h") {
		t.Fatalf("stale crawler warning = %q", warning)
	}
	openModal(t, page, "keywordsBtn", "keywordsModalOverlay")
	page.MustElement("#kw-search_queries").MustInput("n8n, ai automation")
	clickID(page, "saveKeywordsBtn")
	waitUntil(t, func() bool {
		k, _ := svc.Keywords(ctx)
		return len(k["search_queries"]) == 2
	}, "search queries not saved")

	openModal(t, page, "sourcesBtn", "sourcesModalOverlay")
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

// Control response delivery at the HTTP boundary: a refresh can finish after
// editing starts, after a newer refresh, or with a server error.
func TestBoardRefresh(t *testing.T) {
	for _, scenario := range []string{"editing", "out_of_order", "failed_read", "move_select", "move_failed", "focused_select"} {
		t.Run(scenario, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			svc := service.New(st)
			svc.SetDataDir(t.TempDir())
			ctx := context.Background()
			if _, err := svc.AddJob(ctx, service.NewJob{Title: "Original title"}); err != nil {
				t.Fatal(err)
			}
			var holdNext atomic.Bool
			var failingPath atomic.Value
			failingPath.Store("")
			captured, release := make(chan struct{}), make(chan struct{}, 1)
			handler := Handler(st, svc, testToken)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Keep the event stream connected but quiet so only the refreshes
				// deliberately started below affect response ordering.
				if r.URL.Path == "/api/events" {
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				if r.URL.Path == failingPath.Load().(string) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					fmt.Fprint(w, `{"error":"Refresh unavailable <img src=x onerror=window.__pwned=1>"}`)
					return
				}
				if r.URL.Path == "/api/jobs" && holdNext.Swap(false) {
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, r)
					close(captured)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					for k, v := range rec.Header() {
						w.Header()[k] = v
					}
					w.WriteHeader(rec.Code)
					w.Write(rec.Body.Bytes())
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer ts.Close()
			defer close(release)
			page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
			// Close the page before the server, including its live event stream.
			defer page.Close()
			page.MustElement(".card")
			refresh := func() { page.MustEval(`async () => { await refreshAppState(); }`) }
			setTitle := func(title string) {
				if _, err := svc.UpdateJob(ctx, 1, service.Patch{Title: &title}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "failed_read" {
				for _, endpoint := range []string{"jobs", "rating-config", "crawl-status", "activity"} {
					oldTitle := page.MustElement(".card-title").MustText()
					newTitle := "Recovered after " + endpoint
					setTitle(newTitle)
					failingPath.Store("/api/" + endpoint)
					page.MustEval(`() => { document.getElementById('toastContainer').textContent = ''; }`)
					refresh()
					if got := page.MustEval(`() => document.querySelector('.card-title')?.textContent`).Str(); got != oldTitle {
						t.Fatalf("%s failure replaced visible title: got %q, want %q", endpoint, got, oldTitle)
					}
					if toast := page.MustElement("#toastContainer").MustText(); !strings.Contains(toast, "Refresh unavailable <img") {
						t.Fatalf("%s failure not shown: %q", endpoint, toast)
					}
					if page.MustEval(`() => !!window.__pwned || !!document.querySelector('#toastContainer img')`).Bool() {
						t.Fatal("error message interpreted as HTML")
					}
					failingPath.Store("")
					refresh()
					if got := page.MustElement(".card-title").MustText(); got != newTitle {
						t.Fatalf("recovery title = %q, want %q", got, newTitle)
					}
				}
				return
			}
			if scenario == "move_select" {
				// Choosing a stage commits it; focus staying on the select must
				// not hold the saved move back as if it were an unsaved edit.
				page.MustEval(`() => { const s = document.querySelector('.card[data-id="1"] .move-select'); s.focus(); s.value = "Applied"; s.dispatchEvent(new Event("change")); }`)
				if err := page.Timeout(5 * time.Second).Wait(rod.Eval(`() => !!document.querySelector('.column[data-status="Applied"] .card[data-id="1"]')`)); err != nil {
					t.Fatalf("moved card not shown in Applied while the stage select had focus: %v", err)
				}
				return
			}
			if scenario == "move_failed" {
				// A rejected move leaves the focused select showing the stage the
				// job is really in, not the one that failed to save.
				original := page.MustEval(`() => document.querySelector('.card[data-id="1"] .move-select').value`).Str()
				before, _ := svc.GetJob(ctx, 1)
				failingPath.Store("/api/jobs/1")
				page.MustEval(`() => { const s = document.querySelector('.card[data-id="1"] .move-select'); s.focus(); s.value = "Applied"; s.dispatchEvent(new Event("change")); }`)
				if err := page.Timeout(5 * time.Second).Wait(rod.Eval(`() => document.getElementById('toastContainer').textContent.includes("Couldn't move job")`)); err != nil {
					t.Fatalf("failed move not reported: %v", err)
				}
				state := page.MustEval(`() => { const s = document.activeElement; return { focused: s.classList.contains('move-select') && !!s.closest('.card[data-id="1"]'), value: s.value, lane: s.closest('.column')?.dataset.status }; }`)
				if !state.Get("focused").Bool() || state.Get("value").Str() != original || state.Get("lane").Str() != original {
					t.Fatalf("after failed move: %v, want focused select showing %q in lane %q", state, original, original)
				}
				if j, _ := svc.GetJob(ctx, 1); j.Status != before.Status {
					t.Fatalf("persisted status = %q, want %q", j.Status, before.Status)
				}
				return
			}
			if scenario == "focused_select" {
				// A focused stage select with no choice made yet is still in use:
				// a background refresh must wait rather than destroy it.
				page.MustEval(`() => document.querySelector('.card[data-id="1"] .move-select').focus()`)
				setTitle("Background title")
				refresh()
				if !page.MustEval(`() => document.activeElement.classList.contains('move-select') && document.activeElement.closest('.card[data-id="1"]') && document.querySelector('.card-title').textContent === 'Original title'`).Bool() {
					t.Fatal("background refresh replaced the focused stage select")
				}
				page.MustEval(`() => document.activeElement.blur()`)
				page.MustWait(`() => document.querySelector('.card-title')?.textContent === 'Background title'`)
				return
			}
			if scenario == "editing" {
				page.MustEval(`() => toggleCardDetail("1")`)
				page.MustElement("#detail-1 details summary").MustClick()
			}
			holdNext.Store(true)
			page.MustEval(`() => { window.heldRefresh = refreshAppState(); }`)
			select {
			case <-captured:
			case <-time.After(5 * time.Second):
				t.Fatal("jobs response was not captured")
			}
			if scenario == "editing" {
				page.MustElement("#edit-title-1").MustSelectAllText().MustInput("My unsaved title")
			} else {
				setTitle("Newer title")
				refresh()
			}
			release <- struct{}{}
			page.MustEval(`async () => { await window.heldRefresh; }`)
			if scenario == "editing" {
				if !page.MustEval(`() => document.activeElement.id === 'edit-title-1' && document.activeElement.value === 'My unsaved title'`).Bool() {
					t.Fatal("in-flight refresh discarded the edit or focus")
				}
				page.MustElementR("button", "Save details").MustClick()
				waitUntil(t, func() bool {
					j, _ := svc.GetJob(ctx, 1)
					return j.Title == "My unsaved title"
				}, "preserved edit was not saved")
				page.MustWait(`() => document.querySelector('.card-title')?.textContent === 'My unsaved title'`)
			} else if got := page.MustElement(".card-title").MustText(); got != "Newer title" {
				t.Fatalf("older response replaced newer title: %q", got)
			}
		})
	}
}

// Rating is available without A/B mode, honors the selected model, and shows
// saved challenger verdicts without changing the default model's badge.
func TestBoardRating(t *testing.T) {
	for _, scenario := range []string{"normal", "selected_model", "saved_challenger"} {
		t.Run(scenario, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			svc := service.New(st)
			dir := t.TempDir()
			svc.SetDataDir(dir)
			svc.SetJev(apiFakeJev{})
			if err := os.WriteFile(filepath.Join(dir, "profile.md"), []byte("Backend engineer, Go and Postgres."), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			cfg := rating.Config{Models: []string{"default-rater", "rival-one"}, DefaultModel: "default-rater", ABEnabled: scenario != "normal"}
			if scenario == "selected_model" {
				cfg.Models[0], cfg.DefaultModel = jev.DefaultModel, jev.DefaultModel
			}
			if _, err := svc.SetRatingConfig(ctx, cfg); err != nil {
				t.Fatal(err)
			}
			job, err := svc.AddJob(ctx, service.NewJob{Title: "Go Engineer"})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "saved_challenger" {
				for _, v := range []service.Verdict{
					{JobID: job.ID, Model: "default-rater", Score: 4.7, Reasoning: "Strong default match"},
					{JobID: job.ID, Model: "rival-one", Score: 2, Reasoning: `<img src=x onerror="window.__pwned=1"> Missing core skills`},
				} {
					if _, err := svc.SubmitRating(ctx, v); err != nil {
						t.Fatal(err)
					}
				}
			}
			ts := httptest.NewServer(Handler(st, svc, testToken))
			defer ts.Close()
			page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
			page.MustElement(".card")
			page.MustEval(`() => toggleCardDetail("1")`)
			if scenario == "saved_challenger" {
				if text := page.MustElement("#detail-1").MustText(); !strings.Contains(text, "rival-one: F 2") || !strings.Contains(text, "Missing core skills") {
					t.Fatalf("saved challenger missing: %s", text)
				}
				if page.MustEval(`() => !!window.__pwned || !!document.querySelector('#detail-1 img')`).Bool() {
					t.Fatal("challenger reasoning interpreted as HTML")
				}
				if badge := page.MustElement(".card-actions-group > div:first-child > .score-badge:last-child").MustText(); !strings.Contains(badge, "A") || !strings.Contains(badge, "4.7") {
					t.Fatalf("challenger replaced default badge: %q", badge)
				}
				return
			}
			if scenario == "normal" {
				if !page.MustEval(`() => [...document.querySelectorAll('#detail-1 button')].some(b => b.textContent.trim() === 'Rate')`).Bool() {
					t.Fatal("normal card has no Rate action")
				}
				page.MustElementR("#detail-1 button", "^Rate$").MustClick()
			} else {
				page.MustElement("#askModelSelect-1").MustSelect("rival-one")
				page.MustElementR("#detail-1 button", "^Ask Model$").MustClick()
			}
			if err := page.Timeout(5 * time.Second).Wait(rod.Eval(`() => !!document.querySelector('#askModalOverlay.open') || !!document.querySelector('.card .score-badge')?.textContent.includes('4.2')`)); err != nil {
				j, _ := svc.GetJob(ctx, job.ID)
				t.Fatalf("rating request did not finish: %v; score status %q; toast %q", err, j.ScoreStatus, page.MustElement("#toastContainer").MustText())
			}
			wantModel := "default-rater"
			if scenario == "selected_model" {
				wantModel = "rival-one"
			}
			if prompt := page.MustElement("#askPrompt").MustText(); !strings.Contains(prompt, wantModel) {
				t.Fatalf("rating prompt does not name %q: %q", wantModel, prompt)
			}
			if j, _ := svc.GetJob(ctx, job.ID); j.ScoreStatus == "scored" {
				t.Fatal("request for user's AI ran the default Jev rater")
			}
			// Capture the clipboard boundary, so the assertion covers the actual
			// full brief offered by the button without OS clipboard permissions.
			page.MustEval(`() => Object.defineProperty(navigator, 'clipboard', {configurable:true, value:{writeText: async text => { window.copiedBrief = text; }}})`)
			page.MustElementR("#askModalOverlay button", "Copy full brief").MustClick()
			if brief := page.MustEval(`() => window.copiedBrief`).Str(); !strings.Contains(brief, "--model '"+wantModel+"'") {
				t.Fatalf("copied brief loses selected model: %q", brief)
			}
			if scenario == "selected_model" {
				// Reverse the roles: Jev can also be a challenger, and the success
				// toast must describe its score rather than the unscored badge.
				page.MustEval(`() => document.getElementById('askModalOverlay').classList.remove('open')`)
				cfg.DefaultModel = "rival-one"
				if _, err := svc.SetRatingConfig(ctx, cfg); err != nil {
					t.Fatal(err)
				}
				page.MustEval(`async () => { await refreshAppState(); }`)
				page.MustElement("#askModelSelect-1").MustSelect(jev.DefaultModel)
				page.MustElementR("#detail-1 button", "^Ask Model$").MustClick()
				page.MustWait(`() => document.getElementById('toastContainer').textContent.includes('Rated B 4.2')`)
				page.MustWait(`() => document.getElementById('detail-1').textContent.includes('jev-1.13.0: B 4.2')`)
				if j, _ := svc.GetJob(ctx, job.ID); j.ScoreStatus == "scored" {
					t.Fatal("Jev challenger replaced the default badge")
				}
			}
		})
	}
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
// setViewportWidth resizes the page and waits until its script sees the new
// width: Chrome can apply the override after the CDP call returns, and the
// mobile accordion reads window.innerWidth on click.
// waitVisible waits until selector matches a rendered element, looking it up
// afresh each time: the board re-renders its cards, so a handle taken
// earlier can be detached and would never become visible.
func waitVisible(page *rod.Page, selector string) {
	page.MustWait(`sel => {
		const e = document.querySelector(sel);
		return !!e && e.getClientRects().length > 0 && getComputedStyle(e).visibility !== "hidden";
	}`, selector)
}

func setViewportWidth(page *rod.Page, width int) {
	page.MustSetViewport(width, 844, 1, false)
	page.MustWait(`w => window.innerWidth === w`, width)
}

func clickID(page *rod.Page, id string) {
	if _, err := page.Timeout(10 * time.Second).Element("#" + id); err != nil {
		info, ierr := page.Timeout(5 * time.Second).Info()
		html, herr := page.Timeout(5 * time.Second).HTML()
		panic(fmt.Sprintf("#%s not found: %v | url=%v (%v) | html=%.300q (%v)", id, err, info, ierr, html, herr))
	}
	page.MustEval(`id => document.getElementById(id).click()`, id)
}

type boardFakeGeo struct{}

func (boardFakeGeo) Search(_ context.Context, q string, _ int, _ geo.Point) ([]geo.Place, error) {
	if strings.Contains(q, "Landmark") {
		return []geo.Place{{Point: geo.Point{Lat: 10.7949, Lng: 106.7219}, Label: "Landmark 81, Bình Thạnh"}}, nil
	}
	return nil, geo.ErrNotFound
}

// The Location window: "use my current location" (permission granted,
// position faked through the browser), save, and the card shows a distance.
func TestLocationWindow(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	svc.SetGeocoder(boardFakeGeo{})
	ctx := context.Background()
	j, _ := svc.AddJob(ctx, service.NewJob{Title: "Near job"})
	svc.UpdateJob(ctx, j.ID, service.Patch{Location: strPtr("Landmark 81")})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	b := launchBrowser(t)
	proto.BrowserGrantPermissions{Permissions: []proto.BrowserPermissionType{proto.BrowserPermissionTypeGeolocation}}.Call(b)
	page := b.MustPage("")
	proto.EmulationSetGeolocationOverride{Latitude: ptrF(10.7725), Longitude: ptrF(106.6980), Accuracy: ptrF(50)}.Call(page)
	page.MustNavigate(ts.URL).MustWaitLoad()
	if txt := page.MustElement(".card .card-meta").MustText(); strings.Contains(txt, "km") {
		t.Fatalf("before a location is set, card shows a distance badge: %q", txt)
	}

	openModal(t, page, "locationBtn", "locationModalOverlay")
	page.MustElement("#locationModalOverlay.open")
	page.MustElement("#locationMap.leaflet-container")
	clickID(page, "useMyLocationBtn")
	waitUntil(t, func() bool {
		return strings.Contains(page.MustElement("#locationPicked").MustText(), "10.77250")
	}, "browser location not picked")
	page.MustElement("#locationMap .leaflet-marker-icon")

	// Cancelling an unsaved location must discard its visible pin as well.
	page.MustElement("#locationModalOverlay .modal-close-btn").MustEval(`() => this.click()`)
	openModal(t, page, "locationBtn", "locationModalOverlay")
	if page.MustHas("#locationMap .leaflet-marker-icon") {
		t.Fatal("cancelled unsaved location still has a map pin")
	}
	if txt := page.MustElement("#locationPicked").MustText(); txt != "Click the map to place your pin." {
		t.Fatalf("unsaved location prompt = %q", txt)
	}
	if l, err := svc.GetLocation(ctx); err != nil || l.Set {
		t.Fatalf("cancel saved a location: %+v, %v", l, err)
	}
	clickID(page, "useMyLocationBtn")
	waitUntil(t, func() bool {
		return strings.Contains(page.MustElement("#locationPicked").MustText(), "10.77250")
	}, "browser location not picked again")
	page.MustElement("#maxKmInput").MustSelectAllText().MustInput("8")
	clickID(page, "saveLocationBtn")
	waitUntil(t, func() bool { l, _ := svc.GetLocation(ctx); return l.Set && l.MaxKM == 8 }, "location not saved")

	waitUntil(t, func() bool { jj, _ := svc.GetJob(ctx, j.ID); return jj.HasDistance }, "distance not computed")
	page.MustEval(`() => refreshAppState()`)
	waitUntil(t, func() bool {
		return strings.Contains(page.MustElement(".card .card-meta").MustText(), "✅ 3.")
	}, "card doesn't show the distance badge")

	// Address search goes through braveduck's API (fake geocoder here).
	openModal(t, page, "locationBtn", "locationModalOverlay")
	page.MustElement("#placeSearch").MustInput("Landmark 81")
	clickID(page, "placeSearchBtn")
	page.MustElementR("#placeResults button", "Bình Thạnh").MustEval(`() => this.click()`)
	if txt := page.MustElement("#locationPicked").MustText(); !strings.Contains(txt, "Landmark 81") {
		t.Fatalf("search result not picked: %q", txt)
	}
	page.MustElement("#locationModalOverlay .modal-close-btn").MustEval(`() => this.click()`)
	openModal(t, page, "locationBtn", "locationModalOverlay")
	page.MustElement("#locationMap .leaflet-marker-icon")
	if txt := page.MustElement("#locationPicked").MustText(); !strings.Contains(txt, "10.77250") {
		t.Fatalf("cancelled replacement did not restore saved location: %q", txt)
	}
	if l, err := svc.GetLocation(ctx); err != nil || !l.Set || l.Lat != 10.7725 || l.Lng != 106.6980 || l.MaxKM != 8 {
		t.Fatalf("cancel changed the saved location: %+v, %v", l, err)
	}
}

func strPtr(s string) *string { return &s }
func ptrF(f float64) *float64 { return &f }

// openModal clicks a toolbar button until its window is open (up to 5 tries,
// 1 s apart): on rare runs the first click lands while the board is still
// rendering after load and has no visible effect.
func openModal(t *testing.T, page *rod.Page, btnID, overlayID string) {
	t.Helper()
	for i := 0; i < 5; i++ {
		clickID(page, btnID)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			// Short, retried evals: on rare runs one CDP evaluate never
			// returns, while the next one does.
			if v, err := page.Timeout(2*time.Second).Eval(`id => document.getElementById(id).classList.contains("open")`, overlayID); err == nil && v.Value.Bool() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Fatalf("%s didn't open %s", btnID, overlayID)
}

// Pasting a job that's already on the board is refused with a warning that
// names the job (escaped), and "Show" takes the user to its card.
func TestBoardWarnsOnDuplicatePaste(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	svc.AddJob(ctx, service.NewJob{Title: `<img src=x onerror="window.__pwned=1">Analyst`, URL: "https://www.topcv.vn/viec-lam/x/1.html", Source: "topcv"})
	svc.AddJob(ctx, service.NewJob{Title: "Another job", Source: "paste"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	for _, scenario := range []string{"unfiltered", "filtered", "mobile collapsed"} {
		t.Run(scenario, func(t *testing.T) {
			switch scenario {
			case "filtered":
				page.MustElement(`input[name="platform-filter"][value="paste"]`).MustEval(`() => this.click()`)
				page.MustElement("#jobSearch").MustInput("not a matching title")
			case "mobile collapsed":
				setViewportWidth(page, 390)
				page.MustElement("#col-Saved .column-header").MustEval(`() => this.click()`)
				if page.MustElement(`.card[data-id="1"]`).MustVisible() {
					t.Fatal("mobile column did not collapse before Show")
				}
			}
			page.MustElement("#openDrawerBtn").MustEval(`() => this.click()`)
			page.MustElement("#pasteJDTextarea").MustSelectAllText().MustInput("https://www.topcv.vn/viec-lam/x/1.html?ta_source=list")
			page.MustElement("#extractAddBtn").MustEval(`() => this.click()`)

			page.MustElement("#extractResultBanner").MustWaitVisible()
			banner := page.MustElement("#extractResultBanner li")
			if txt := banner.MustText(); !strings.Contains(txt, "#1") || !strings.Contains(txt, "Analyst") || !strings.Contains(txt, "(saved)") {
				t.Fatalf("banner = %q, want it to name #1", txt)
			}
			if !page.MustEval(`() => window.__pwned === undefined && !document.querySelector("#extractResultBanner img")`).Bool() {
				t.Fatal("duplicate's title injected markup into the banner")
			}
			if list, _ := svc.ListJobs(ctx, service.ListFilter{}); len(list) != 2 {
				t.Fatalf("%d jobs, want the duplicate refused", len(list))
			}

			banner.MustElement("button").MustEval(`() => this.click()`)
			if !page.MustEval(`() => !!document.querySelector('.card[data-id="1"] #detail-1')`).Bool() {
				t.Fatal("Show did not reveal and expand the duplicate card")
			}
			waitVisible(page, "#detail-1")
			if page.MustEval(`() => document.getElementById("addJobDrawer").classList.contains("open")`).Bool() {
				t.Fatal("Add drawer still open after Show")
			}
			if scenario == "filtered" && !page.MustEval(`() => document.getElementById('jobSearch').value === '' && document.querySelector('input[name="platform-filter"][value="All"]').checked`).Bool() {
				t.Fatal("Show left the filter controls out of sync with the revealed card")
			}
		})
		if t.Failed() {
			break
		}
	}
}

// My CV: upload a CV file from the board; its name is shown escaped, its
// text lands in the profile, and a second upload asks before replacing.
func TestBoardUploadsCV(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	svc := service.New(st)
	svc.SetDataDir(dir)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	files := t.TempDir()
	first := filepath.Join(files, `<img src=x onerror="window.__pwned=1">.md`)
	second := filepath.Join(files, "cv-2026.md")
	os.WriteFile(first, []byte("# Nguyễn Văn Khang\n\nMarketing AI, 3 năm kinh nghiệm với n8n tại Vịt Vàng."), 0o600)
	os.WriteFile(second, []byte("# Nguyễn Văn Khang\n\nData analyst, 5 năm kinh nghiệm SQL và Python tại Duck Pay."), 0o600)

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement("#myCVBtn").MustEval(`() => this.click()`)
	page.MustElementR("#cvStatus", "No CV uploaded yet")

	page.MustElement("#cvFileInput").MustSetFiles(first)
	page.MustElement("#cvUploadBtn").MustEval(`() => this.click()`)
	page.MustElementR("#cvResult", "uploaded")
	page.MustElementR("#cvStatus", "is filled in")
	if !page.MustEval(`() => window.__pwned === undefined && !document.querySelector("#cvModalOverlay img")`).Bool() {
		t.Fatal("the CV's file name injected markup")
	}

	// The second upload asks before replacing what's in About me.
	wait, handle := page.MustHandleDialog()
	page.MustElement("#cvFileInput").MustSetFiles(second)
	go func() {
		defer func() { _ = recover() }() // see TestLoginsPanel: a panic here would end the whole run
		page.MustElement("#cvUploadBtn").MustEval(`() => this.click()`)
	}()
	if d := wait(); !strings.Contains(d.Message, "About me") {
		t.Fatalf("dialog = %q", d.Message)
	}
	handle(true, "")
	page.MustElementR("#cvStatus", "cv-2026.md")

	b, err := os.ReadFile(filepath.Join(dir, "profile.md"))
	if err != nil || !strings.Contains(string(b), "Duck Pay") || strings.Contains(string(b), "Vịt Vàng") {
		t.Fatalf("profile after replace (%v):\n%s", err, b)
	}
}

// Get started: a fresh board lists the steps; the profile editor saves to the
// profile file, and the card goes away once the needed steps are done.
func TestBoardGetStarted(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	svc := service.New(st)
	svc.SetDataDir(dir)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()
	ctx := context.Background()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElementR("#setupSteps", "Say what you're looking for")
	if n := page.MustEval(`() => document.querySelectorAll("#setupSteps button").length`).Int(); n != 3 {
		t.Fatalf("%d open steps on a fresh board, want 3", n)
	}

	// Step 2 opens the profile form: no Markdown to write.
	page.MustElement(`#setupSteps button[data-setup="wants"]`).MustEval(`() => this.click()`)
	page.MustElementR("#profileStatus", "Not filled in yet")
	waitVisible(page, "#profileFormBody .pf-tag-input")
	if page.MustEval(`() => document.getElementById("profileTextBody").hidden`).Bool() == false {
		t.Fatal("the Markdown editor shows first")
	}
	roles := page.MustElement(`#profileFormBody input[aria-label="Job titles"]`)
	roles.MustInput("Marketing AI")
	roles.MustType(input.Enter)
	roles.MustInput("AI automation")
	roles.MustType(input.Enter)
	page.MustElementR("#profileFormBody .pf-choice", "^Remote$").MustEval(`() => this.click()`)
	page.MustElementR("#profileFormBody .pf-suggestion", "Working on Saturdays").MustEval(`() => this.click()`)
	page.MustElement(`#profileFormBody input[aria-label="The least you'd accept amount"]`).MustInput("15000000")
	page.MustElement(`#profileFormBody textarea[aria-label="Your experience and skills"]`).MustInput("Marketing executive, 3 years of AI automation with n8n.")
	clickID(page, "saveProfileBtn")
	page.MustElementR("#profileStatus", "Filled in")
	b, _ := os.ReadFile(filepath.Join(dir, "profile.md"))
	for _, want := range []string{
		"- Target roles / job titles: Marketing AI, AI automation\n",
		"- Remote / hybrid / on-site preference: Remote\n",
		"- Minimum I'll accept: 15,000,000 VND per month\n",
		"- Working on Saturdays\n",
		"Marketing executive, 3 years of AI automation with n8n.",
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("profile file missing %q:\n%s", want, b)
		}
	}
	// "Edit as text" still shows the whole file.
	clickID(page, "profileModeBtn")
	page.MustWait(`() => !document.getElementById("profileTextBody").hidden`)
	if v := page.MustElement("#profileText").MustProperty("value").Str(); !strings.Contains(v, "Marketing AI, AI automation") {
		t.Fatalf("text editor = %.200q", v)
	}
	page.MustElement(`#setupSteps li.done`)

	// The last needed step (search terms, here added over the API) hides the card.
	svc.SetKeywords(ctx, map[string][]string{"search_queries": {"n8n"}})
	page.MustEval(`() => refreshAppState()`)
	deadline := time.Now().Add(5 * time.Second)
	for !page.MustEval(`() => document.getElementById("setupCard").hidden`).Bool() {
		if time.Now().After(deadline) {
			t.Fatal("Get started still shown once About me, what they want and search terms are all set")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Search narrows the board as you type (accents optional), "/" jumps to it;
// cards show đồng salaries in triệu and no distance badge before a location
// is set.
func TestBoardSearchAndCards(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	svc.AddJob(ctx, service.NewJob{Title: "Kỹ Sư AI", Company: "Sao Mai", Salary: "16000000 - 22000000 VND/month"})
	svc.AddJob(ctx, service.NewJob{Title: "Data Analyst", Company: "Đà Nẵng Duck Co"})
	svc.AddJob(ctx, service.NewJob{Title: "Marketing Executive", Company: "Awing"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	cards := func() string {
		return page.MustEval(`() => [...document.querySelectorAll(".card .card-title")].map(e => e.textContent.trim()).sort().join("|")`).Str()
	}
	if got := cards(); got != "Data Analyst|Kỹ Sư AI|Marketing Executive" {
		t.Fatalf("cards = %q", got)
	}
	if chip := page.MustElement(".salary-chip").MustText(); !strings.Contains(chip, "16–22 triệu/tháng") {
		t.Fatalf("salary chip = %q", chip)
	}
	if page.MustEval(`() => document.body.innerText.includes("? km")`).Bool() {
		t.Fatal("a distance badge is shown with no location set")
	}

	page.Keyboard.MustType('/')
	if !page.MustEval(`() => document.activeElement.id === "jobSearch"`).Bool() {
		t.Fatal(`"/" didn't focus the search box`)
	}
	page.MustElement("#jobSearch").MustInput("ky su")
	if got := cards(); got != "Kỹ Sư AI" {
		t.Fatalf(`"ky su" shows %q, want only the accented match`, got)
	}
	page.MustElement("#jobSearch").MustSelectAllText().MustInput("da nang duck")
	if got := cards(); got != "Data Analyst" {
		t.Fatalf(`"da nang duck" shows %q, want the company match`, got)
	}
	page.Keyboard.MustType(input.Escape)
	if got := cards(); got != "Data Analyst|Kỹ Sư AI|Marketing Executive" {
		t.Fatalf("after Esc = %q, want every card back", got)
	}
}

// Today lists what to act on: the best-rated open jobs, deadlines this week
// and week-old applications; a click opens the card.
func TestBoardToday(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	now := time.Now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	for _, j := range []jobs.Job{
		{Title: "Great match", Status: "saved", Score: 4.6, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"},
		{Title: "Weak match", Status: "saved", Score: 2.1, HasScore: true, ScoreLetter: "D", ScoreStatus: "scored"},
		{Title: "Closes soon", Status: "saved", ExpiresAt: "expire:" + day(2)},
		{Title: "Closes next month", Status: "saved", ExpiresAt: "expire:" + day(30)},
		{Title: "Waiting for an answer", Status: "applied", ExpiresAt: "applied:" + day(-10)},
		{Title: "Just applied", Status: "applied", ExpiresAt: "applied:" + day(-2)},
	} {
		if _, err := st.Add(ctx, j, now); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	today := page.MustElement("#todayList").MustText()
	for _, want := range []string{"Great match", "A · 4.6", "Closes soon", "closes in 2 days", "Waiting for an answer", "applied 10 days ago"} {
		if !strings.Contains(today, want) {
			t.Errorf("Today is missing %q:\n%s", want, today)
		}
	}
	for _, not := range []string{"Weak match", "Closes next month", "Just applied"} {
		if strings.Contains(today, not) {
			t.Errorf("Today lists %q:\n%s", not, today)
		}
	}

	page.MustElementR("#todayList button", "Closes soon").MustEval(`() => this.click()`)
	page.MustElement(`.card.flash`)
}

// ⚙ Settings opens a menu of the less-used windows; picking one closes the
// menu and opens the window, and Esc closes the menu.
func TestBoardSettingsMenu(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement("#settingsBtn").MustEval(`() => this.click()`)
	page.MustElement(`#settingsBtn[aria-expanded="true"]`)
	if items := page.MustElement("#settingsMenu").MustText(); !strings.Contains(items, "Search terms") || !strings.Contains(items, "Job-site logins") {
		t.Fatalf("menu = %q", items)
	}
	page.MustElement("#sourcesBtn").MustEval(`() => this.click()`)
	page.MustElement("#sourcesModalOverlay.open")
	if !page.MustEval(`() => document.getElementById("settingsMenu").hidden`).Bool() {
		t.Fatal("menu still open after a pick")
	}
	page.MustEval(`() => document.querySelector("#sourcesModalOverlay .modal-close-btn").click()`)

	page.MustElement("#settingsBtn").MustEval(`() => this.click()`)
	page.Keyboard.MustType(input.ArrowDown)
	if id := page.MustEval(`() => document.activeElement.id`).Str(); id != "sourcesBtn" {
		t.Fatalf("ArrowDown focused %q, want the second item", id)
	}
	page.Keyboard.MustType(input.Escape)
	if !page.MustEval(`() => document.getElementById("settingsMenu").hidden && document.activeElement.id === "settingsBtn"`).Bool() {
		t.Fatal("Esc didn't close the menu and return focus")
	}
}

// Great-match notifications: switching them on doesn't notify for matches
// already on the board; a job rated A afterwards does, once.
func TestBoardNotifiesNewMatches(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	st.Add(ctx, jobs.Job{Title: "Old match", Status: "saved", Score: 4.5, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"}, time.Now())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	// A stand-in Notification that records what the board shows.
	page.MustEvalOnNewDocument(`window.__shown = [];
		window.Notification = class { constructor(title, opts) { window.__shown.push(title); } close() {} };
		Notification.permission = "granted";
		Notification.requestPermission = async () => "granted";`)
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")

	page.MustEval(`() => toggleNotify()`)
	page.MustElementR("#notifyBtn", "great matches: on")
	if n := page.MustEval(`() => window.__shown.length`).Int(); n != 0 {
		t.Fatalf("%d notifications on switching on, want none for existing matches", n)
	}

	st.Add(ctx, jobs.Job{Title: "Fresh match", Company: "Duck Co", Status: "saved", Score: 4.8, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"}, time.Now())
	page.MustEval(`() => refreshAppState()`)
	page.MustEval(`() => refreshAppState()`) // a second refresh mustn't repeat it
	if got := page.MustEval(`() => window.__shown.join("|")`).Str(); got != "Great match: Fresh match" {
		t.Fatalf("notifications = %q, want one for the new match", got)
	}
}

// Expiry badges match the sweep: Saved jobs count down from their deadline,
// Applied ones from the day they were sent; Interview cards have no clock.
func TestBoardExpiryBadges(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	now := time.Now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	for _, j := range []jobs.Job{
		{Title: "Posting closed", Status: "saved", ExpiresAt: "expire:" + day(-3)},
		{Title: "No reply", Status: "applied", ExpiresAt: "applied:" + day(-22)},
		{Title: "Just applied", Status: "applied", ExpiresAt: "applied:" + day(-4)},
		{Title: "Interviewing", Status: "interview", ExpiresAt: "applied:" + day(-60)},
	} {
		st.Add(ctx, j, now)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	badge := func(title string) string {
		return page.MustEval(`t => {
			const card = [...document.querySelectorAll(".card")].find(c => c.querySelector(".card-title").textContent.trim() === t);
			const b = card && card.querySelector(".expiry-badge");
			return b ? b.textContent.trim() : "";
		}`, title).Str()
	}
	for title, want := range map[string]string{
		"Posting closed": "⛔ to Closed in 4d",
		"No reply":       "⛔ to Closed in 5d",
		"Just applied":   "📮 4d",
		"Interviewing":   "",
	} {
		if got := badge(title); got != want {
			t.Errorf("%s: badge %q, want %q", title, got, want)
		}
	}
	if page.MustEval(`() => document.body.innerText.includes("delete in")`).Bool() {
		t.Error(`a card still says "delete in" — nothing is ever deleted`)
	}
}

// Background tasks (Jev rating, TopCV sync) get no source chip and no filter
// checkbox: they don't find jobs.
func TestBoardHidesTaskSources(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	for _, src := range []string{"careerlink", service.TopCVSyncSource, service.JevRateSource} {
		st.RecordCrawlRun(ctx, src, true, "", time.Now())
	}
	svc.AddJob(ctx, service.NewJob{Title: "A job", Source: "careerlink"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	chips := page.MustElement("#crawlChips").MustText()
	filters := page.MustElement("#platformFilters").MustText()
	if !strings.Contains(chips, "careerlink") || !strings.Contains(filters, "careerlink") {
		t.Fatalf("job source missing: chips %q, filters %q", chips, filters)
	}
	for _, task := range []string{service.TopCVSyncSource, service.JevRateSource} {
		if strings.Contains(chips, task) || strings.Contains(filters, task) {
			t.Errorf("task %q shown as a source: chips %q, filters %q", task, chips, filters)
		}
	}
}

// Assisted apply works on any site with a saved login: the card offers
// "Apply on VietnamWorks", and after the window it asks whether you
// submitted — "Yes, I applied" moves the job to Applied.
func TestBoardApplyOnAnySite(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	st.SaveSession(ctx, store.PlatformSession{Platform: "vietnamworks", Enc: []byte{1}, CreatedAt: "x", FPSeed: "1", Status: store.SessionConnected})
	ready, _ := svc.AddJob(ctx, service.NewJob{Title: "Growth Marketer", URL: "https://www.vietnamworks.com/growth-marketer-1-jv"})
	svc.SubmitCV(ctx, ready.ID, "# A\n\n## Experience\n- Growth marketing with SQL and Python for five years")
	asked, _ := svc.AddJob(ctx, service.NewJob{Title: "Data Analyst", URL: "https://www.vietnamworks.com/data-analyst-2-jv"})
	st.SetApplyState(ctx, asked.ID, "confirm", "did you submit it on VietnamWorks?", time.Now())
	noLogin, _ := svc.AddJob(ctx, service.NewJob{Title: "On JobOKO", URL: "https://vn.joboko.com/viec-lam-x-xvi1"})
	svc.SubmitCV(ctx, noLogin.ID, "# A\n\n## Experience\n- Growth marketing with SQL and Python for five years")
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	for _, id := range []int64{ready.ID, noLogin.ID} {
		page.MustEval(`id => toggleCardDetail(String(id))`, id)
	}
	waitUntil(t, func() bool {
		return page.MustEval(`() => [...document.querySelectorAll("button")].some(b => b.textContent === "Apply on VietnamWorks")`).Bool()
	}, "no Apply on VietnamWorks button")
	if page.MustEval(`() => [...document.querySelectorAll("button")].some(b => /Apply on (JobOKO|TopCV)/.test(b.textContent))`).Bool() {
		t.Fatal("an Apply button for a site without a login")
	}

	page.MustElementR(".apply-confirm", "Did you submit it on VietnamWorks").MustElementR("button", "Yes, I applied").MustEval(`() => this.click()`)
	waitUntil(t, func() bool { j, _ := svc.GetJob(ctx, asked.ID); return j.Status == "applied" && j.ApplyState == "sent" }, "confirming didn't move the job to Applied")
}

// A point picked while the map library is still loading (your location, a
// search result) gets its pin once the map is ready, instead of being
// replaced by the saved location.
func TestLocationPickWhileMapLoads(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	svc.SetGeocoder(boardFakeGeo{})
	ctx := context.Background()
	if _, err := svc.SetLocation(ctx, geo.Point{Lat: 21.0285, Lng: 105.8542}, "Hà Nội (saved)", 10); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card, #kanbanBoard")
	// Load the real library, then make the window's wait for it hang until
	// released: the window opens with no map yet.
	page.MustEval(`async () => { await loadLeaflet(); leafletReady = new Promise(r => window.__releaseMap = () => r()); }`)
	page.MustEval(`() => { openLocationWindow(); }`)
	page.MustElement("#locationModalOverlay.open")

	page.MustElement("#placeSearch").MustInput("Landmark 81")
	clickID(page, "placeSearchBtn")
	page.MustElementR("#placeResults button", "Bình Thạnh").MustEval(`() => this.click()`)
	page.MustEval(`() => window.__releaseMap()`)

	page.MustElement("#locationMap .leaflet-marker-icon")
	if txt := page.MustElement("#locationPicked").MustText(); !strings.Contains(txt, "Landmark 81") {
		t.Fatalf("pick made while the map loaded was replaced: %q", txt)
	}
	if got := page.MustEval(`() => { const p = locMarker.getLatLng(); return p.lat.toFixed(4) + "," + p.lng.toFixed(4); }`).Str(); got != "10.7949,106.7219" {
		t.Fatalf("pin at %s, want the picked place", got)
	}
}

// Every button has a name a screen reader can say: its text or aria-label
// must contain a letter (an icon such as ×, ✕, ▾ or 📍 alone isn't one).
func TestBoardButtonsHaveNames(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	j, _ := svc.AddJob(ctx, service.NewJob{Title: "Data Analyst", URL: "https://www.topcv.vn/viec-lam/x/1.html"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustEval(`id => toggleCardDetail(String(id))`, j.ID)
	page.MustElement(fmt.Sprintf("#detail-%d", j.ID))
	unnamed := page.MustEval(`() => [...document.querySelectorAll("button, a.icon-btn-sm")]
		.map(b => ({ name: (b.getAttribute("aria-label") || b.textContent || "").trim(), html: b.outerHTML.slice(0, 120) }))
		.filter(b => !/\p{L}/u.test(b.name))
		.map(b => b.html)`).Arr()
	if len(unnamed) > 0 {
		t.Fatalf("%d button(s) without a spoken name, e.g. %v", len(unnamed), unnamed[0])
	}
	if label := page.MustElement(".card .expand-btn").MustAttribute("aria-label"); label == nil || *label != "Details: Data Analyst" {
		t.Fatalf("expand button label = %v", label)
	}
}
