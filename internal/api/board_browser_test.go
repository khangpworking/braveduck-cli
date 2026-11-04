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
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
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
			page.MustElement(".drawer").MustWaitStable()
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
	clickID(page, "useMyLocationBtn")
	waitUntil(t, func() bool {
		return strings.Contains(page.MustElement("#locationPicked").MustText(), "10.77250")
	}, "browser location not picked")
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
	page.MustElementR("#placeResults button", "Bình Thạnh").MustClick()
	if txt := page.MustElement("#locationPicked").MustText(); !strings.Contains(txt, "Landmark 81") {
		t.Fatalf("search result not picked: %q", txt)
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
	svc.AddJob(ctx, service.NewJob{Title: `<img src=x onerror="window.__pwned=1">Analyst`, URL: "https://www.topcv.vn/viec-lam/x/1.html"})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustElement("#openDrawerBtn").MustClick()
	page.MustElement("#pasteJDTextarea").MustInput("https://www.topcv.vn/viec-lam/x/1.html?ta_source=list")
	page.MustElement("#extractAddBtn").MustClick()

	banner := page.MustElement("#extractResultBanner li")
	if txt := banner.MustText(); !strings.Contains(txt, "#1") || !strings.Contains(txt, "Analyst") || !strings.Contains(txt, "(saved)") {
		t.Fatalf("banner = %q, want it to name #1", txt)
	}
	if !page.MustEval(`() => window.__pwned === undefined && !document.querySelector("#extractResultBanner img")`).Bool() {
		t.Fatal("duplicate's title injected markup into the banner")
	}
	if list, _ := svc.ListJobs(ctx, service.ListFilter{}); len(list) != 1 {
		t.Fatalf("%d jobs, want the duplicate refused", len(list))
	}

	banner.MustElement("button").MustClick()
	page.MustElement(`#detail-1`)
	if page.MustEval(`() => document.getElementById("addJobDrawer").classList.contains("open")`).Bool() {
		t.Fatal("Add drawer still open after Show")
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
	page.MustElement("#myCVBtn").MustClick()
	page.MustElementR("#cvStatus", "No CV uploaded yet")

	page.MustElement("#cvFileInput").MustSetFiles(first)
	page.MustElement("#cvUploadBtn").MustClick()
	page.MustElementR("#cvResult", "uploaded")
	page.MustElementR("#cvStatus", "is filled in")
	if !page.MustEval(`() => window.__pwned === undefined && !document.querySelector("#cvModalOverlay img")`).Bool() {
		t.Fatal("the CV's file name injected markup")
	}

	// The second upload asks before replacing what's in About me.
	wait, handle := page.MustHandleDialog()
	page.MustElement("#cvFileInput").MustSetFiles(second)
	go page.MustElement("#cvUploadBtn").MustClick()
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

	// Step 2 opens the editor on the real profile template.
	page.MustElement(`#setupSteps button[data-setup="wants"]`).MustClick()
	page.MustElementR("#profileStatus", "Still empty")
	text := page.MustElement("#profileText")
	if v := text.MustProperty("value").Str(); !strings.Contains(v, "# What I'm looking for") {
		t.Fatalf("editor = %.80q, want the profile template", v)
	}
	page.MustEval(`() => { const t = document.getElementById("profileText");
		t.value = t.value.replace("- Target roles / job titles:", "- Target roles / job titles: Marketing AI, AI automation")
			.replace("# What I'm looking for", "Marketing executive, 3 years of AI automation with n8n.\n\n# What I'm looking for"); }`)
	clickID(page, "saveProfileBtn")
	page.MustElementR("#profileStatus", "Filled in")
	b, _ := os.ReadFile(filepath.Join(dir, "profile.md"))
	if !strings.Contains(string(b), "Target roles / job titles: Marketing AI, AI automation") {
		t.Fatalf("profile file not saved:\n%s", b)
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

	page.MustElementR("#todayList button", "Closes soon").MustClick()
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
	page.MustElement("#settingsBtn").MustClick()
	page.MustElement(`#settingsBtn[aria-expanded="true"]`)
	if items := page.MustElement("#settingsMenu").MustText(); !strings.Contains(items, "Search terms") || !strings.Contains(items, "Job-site logins") {
		t.Fatalf("menu = %q", items)
	}
	page.MustElement("#sourcesBtn").MustClick()
	page.MustElement("#sourcesModalOverlay.open")
	if !page.MustEval(`() => document.getElementById("settingsMenu").hidden`).Bool() {
		t.Fatal("menu still open after a pick")
	}
	page.MustEval(`() => document.querySelector("#sourcesModalOverlay .modal-close-btn").click()`)

	page.MustElement("#settingsBtn").MustClick()
	page.Keyboard.MustType(input.ArrowDown)
	if id := page.MustEval(`() => document.activeElement.id`).Str(); id != "sourcesBtn" {
		t.Fatalf("ArrowDown focused %q, want the second item", id)
	}
	page.Keyboard.MustType(input.Escape)
	if !page.MustEval(`() => document.getElementById("settingsMenu").hidden && document.activeElement.id === "settingsBtn"`).Bool() {
		t.Fatal("Esc didn't close the menu and return focus")
	}
}
