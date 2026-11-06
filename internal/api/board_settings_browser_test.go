//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// settingsBoard is a board with temp data and no real crawling.
func settingsBoard(t *testing.T, setup func(svc *service.Service, st *store.Store)) (*rod.Page, *service.Service, string) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	svc := service.New(st)
	svc.SetDataDir(dir)
	if setup != nil {
		setup(svc, st)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	t.Cleanup(ts.Close)
	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	return page, svc, dir
}

// onScreen is what the user can read in an element: a closed "More" or
// "Advanced" doesn't count.
func onScreen(page *rod.Page, selector string) string {
	return page.MustEval(`sel => document.querySelector(sel).innerText`, selector).Str()
}

func openRatingAdvanced(page *rod.Page) {
	page.MustEval(`() => document.querySelector("#ratingAdvanced summary").click()`)
	waitVisible(page, "#defaultModelInput")
}

func assertSays(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("%s doesn't say %q:\n%s", what, w, text)
		}
	}
}

func assertNotSays(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(strings.ToLower(text), strings.ToLower(w)) {
			t.Errorf("%s still says %q:\n%s", what, w, text)
		}
	}
}

// assertFitsPhone checks an open window at phone width: nothing wider than
// the screen, and its buttons on the screen.
func assertFitsPhone(t *testing.T, page *rod.Page, overlay string) {
	t.Helper()
	setViewportWidth(page, 390)
	defer setViewportWidth(page, 1280)
	bad := page.MustEval(`id => {
		const out = [];
		const modal = document.querySelector("#" + id + " .modal");
		if (modal.getBoundingClientRect().right > innerWidth + 1) out.push("the window");
		for (const e of modal.querySelectorAll("button, select, input:not([type=file]), summary")) {
			const r = e.getBoundingClientRect();
			if (r.width && (r.left < 0 || r.right > innerWidth + 1)) out.push(e.outerHTML.slice(0, 60));
		}
		return out.join(" | ");
	}`, overlay).Str()
	if bad != "" {
		t.Errorf("wider than a phone in %s: %s", overlay, bad)
	}
}

// Search terms: one question as tags, Enter adds and keeps the cursor there,
// a one-line count, and the filter lists wait under More. The API is unchanged.
func TestSearchTermsWindow(t *testing.T) {
	ctx := context.Background()
	page, svc, _ := settingsBoard(t, func(svc *service.Service, _ *store.Store) {
		svc.AddJob(ctx, service.NewJob{Title: "Kế toán tổng hợp", Company: "Vịt Vàng"})
		svc.AddJob(ctx, service.NewJob{Title: "Barista", Company: "Duck Cafe"})
		svc.SetKeywords(ctx, keyword.Sets{"negative_company": {"Bad Co"}})
	})
	page.MustWait(`() => AppState.jobs.length === 2`)

	openModal(t, page, "keywordsBtn", "keywordsModalOverlay")
	text := onScreen(page, "#keywordsModalOverlay")
	assertSays(t, "the window", text, "What jobs should we look for?", "More filters (1 in use)")
	assertNotSays(t, "the window", text, "crawler", "comma", "Hide jobs", "preview", "Bad Co")

	in := page.MustElement(`#keywordsForm input[aria-label="What jobs should we look for?"]`)
	in.MustFocus()
	in.MustInput("ke toan")
	page.Keyboard.MustType(input.Enter)
	page.MustElementR("#keywordsForm .pf-tag", "ke toan")
	if !page.MustEval(`() => document.activeElement.classList.contains("pf-tag-input")`).Bool() {
		t.Fatal("Enter took the cursor out of the box: the next term needs another click")
	}
	// Diacritics don't matter, and the preview is one line, not a job list.
	if got := onScreen(page, "#keywordsCount"); got != "1 job on your board mentions these." {
		t.Fatalf("count = %q", got)
	}
	in.MustInput(`<img src=x onerror="window.__pwned=1">`)
	page.Keyboard.MustType(input.Enter)
	page.MustElementR("#keywordsForm .pf-tag", "onerror")
	if !page.MustEval(`() => window.__pwned === undefined && !document.querySelector("#keywordsForm img")`).Bool() {
		t.Fatal("a term injected markup")
	}
	page.MustEval(`() => document.querySelector('#keywordsForm .pf-tag-x[aria-label^="Remove <img"]').click()`)
	page.MustWait(`() => document.querySelectorAll("#keywordsForm > .pf-field .pf-tag").length === 1`)
	if !page.MustEval(`() => document.activeElement.classList.contains("pf-tag-input")`).Bool() {
		t.Fatal("removing a term by keyboard lost the cursor")
	}

	page.MustEval(`() => document.querySelector("#keywordsForm details summary").click()`)
	text = onScreen(page, "#keywordsModalOverlay")
	assertSays(t, "More", text, "Hide jobs that mention…", "Bad Co")
	page.MustElement(`#keywordsForm input[aria-label="Hide jobs that mention…"]`).MustInput("đa cấp")
	assertFitsPhone(t, page, "keywordsModalOverlay")

	clickID(page, "saveKeywordsBtn")
	waitUntil(t, func() bool {
		k, _ := svc.Keywords(ctx)
		return strings.Join(k["search_queries"], "|") == "ke toan" && strings.Join(k["negative_content"], "|") == "đa cấp" &&
			strings.Join(k["negative_company"], "|") == "Bad Co"
	}, "the lists weren't all saved")
}

// countingCrawler counts its searches, so a test sees which sites were checked.
type countingCrawler struct {
	id    string
	calls atomic.Int32
}

func (c *countingCrawler) Name() string { return c.id }
func (c *countingCrawler) Search(context.Context, string) ([]crawl.Listing, error) {
	c.calls.Add(1)
	return []crawl.Listing{{Title: "Job at " + c.id, Company: "Duck Co", URL: "https://www." + c.id + ".com/x-1-jv"}}, nil
}

// Job sources: real site names, plain words and one Check now. Background
// tasks wait under Advanced, and Check now says why when it can't run.
func TestJobSourcesWindow(t *testing.T) {
	ctx := context.Background()
	vnw, cl := &countingCrawler{id: "vietnamworks"}, &countingCrawler{id: "careerlink"}
	page, svc, _ := settingsBoard(t, func(svc *service.Service, st *store.Store) {
		svc.SetCrawlers(vnw, cl)
		svc.SetCrawlSchedule(ctx, "vietnamworks", 120)
		svc.SetCrawlSchedule(ctx, "careerlink", 0)
		st.RecordCrawlRun(ctx, "vietnamworks", true, "", time.Now().Add(-5*time.Minute))
	})

	openModal(t, page, "sourcesBtn", "sourcesModalOverlay")
	page.MustElement("#sourcesList .source-row")
	text := onScreen(page, "#sourcesModalOverlay")
	assertSays(t, "the window", text, "VietnamWorks", "CareerLink", "Last checked 5 minutes ago", "Not checked yet", "Check for new jobs:", "every 2 hours", "Check now")
	assertNotSays(t, "the window", text, "jev-rate", "topcv-sync", "Automatic job rating", "built-in", "never run", "Crawl", "own tool")
	if strings.Contains(text, "vietnamworks") || strings.Contains(text, "careerlink") {
		t.Errorf("the window shows a source's id, not its name:\n%s", text)
	}

	// No search terms yet: the reason instead of a failing check.
	if !page.MustElement("#checkSourcesBtn").MustProperty("disabled").Bool() {
		t.Fatal("Check now works with no search terms")
	}
	assertSays(t, "the hint", onScreen(page, "#sourcesHint"), "what jobs to look for")
	page.MustEval(`() => document.querySelector("#sourcesHint button").click()`)
	page.MustWait(`() => document.getElementById("keywordsModalOverlay").classList.contains("open") && !document.getElementById("sourcesModalOverlay").classList.contains("open")`)
	page.MustEval(`() => document.querySelector("#keywordsModalOverlay .modal-close-btn").click()`)
	svc.SetKeywords(ctx, keyword.Sets{"search_queries": {"n8n"}})

	openModal(t, page, "sourcesBtn", "sourcesModalOverlay")
	page.MustWait(`() => !document.getElementById("checkSourcesBtn").disabled`)
	assertFitsPhone(t, page, "sourcesModalOverlay")

	// Advanced keeps the rest, in words.
	page.MustEval(`() => document.querySelector("#sourcesList details summary").click()`)
	assertSays(t, "Advanced", onScreen(page, "#sourcesList details"), "Automatic job rating", "TopCV application updates")

	// Check now checks the sites that are on; off means off.
	clickID(page, "checkSourcesBtn")
	waitUntil(t, func() bool {
		l, _ := svc.ListJobs(ctx, service.ListFilter{Source: "vietnamworks"})
		return len(l) == 1
	}, "Check now didn't add the job")
	page.MustElementR("#toastContainer", "Found 1 new job")
	page.MustWait(`() => document.getElementById("checkSourcesBtn").textContent === "Check now"`)
	if cl.calls.Load() != 0 {
		t.Fatal("Check now checked a site that's off")
	}

	// The arrow keys change a schedule without losing their place in the list.
	sel := page.MustElement(`#sourcesList select[aria-label^="CareerLink"]`)
	sel.MustFocus()
	page.Keyboard.MustType(input.ArrowDown)
	waitUntil(t, func() bool {
		for _, sc := range must(svc.CrawlSchedules(ctx)) {
			if sc.Source == "careerlink" {
				return sc.EveryMinutes > 0
			}
		}
		return false
	}, "the arrow key didn't change the schedule")
	if !page.MustEval(`() => (document.activeElement.getAttribute("aria-label") || "").startsWith("CareerLink")`).Bool() {
		t.Fatal("changing the schedule lost the keyboard's place")
	}

	// A failed check reads as words, not as the site's raw error.
	svc.ReportCrawl(ctx, "careerlink", false, "HTTP 503 from upstream")
	page.MustEval(`() => renderSources()`)
	page.MustElementR("#sourcesList", "The last check didn't work")
	assertNotSays(t, "the site row", onScreen(page, "#sourcesList > .source-row"), "HTTP 503")

	// All sites off: Check now says so.
	svc.SetCrawlSchedule(ctx, "careerlink", 0)
	svc.SetCrawlSchedule(ctx, "vietnamworks", 0)
	page.MustEval(`() => renderSources()`)
	page.MustElementR("#sourcesHint", "All sites are off")
	if !page.MustElement("#checkSourcesBtn").MustProperty("disabled").Bool() {
		t.Fatal("Check now enabled with every site off")
	}
}

// On a shared server the window offers nothing shorter than an hour.
func TestJobSourcesRespectHostedMinimum(t *testing.T) {
	page, _, _ := settingsBoard(t, func(svc *service.Service, _ *store.Store) {
		svc.SetCrawlers(&countingCrawler{id: "vietnamworks"})
		svc.SetHosted(service.HostedInfo{User: "alice"})
	})
	openModal(t, page, "sourcesBtn", "sourcesModalOverlay")
	page.MustElement("#sourcesList .source-row")
	short := page.MustEval(`() => [...document.querySelectorAll("#sourcesList > .source-row option")]
		.filter(o => o.value !== "" && Number(o.value) > 0 && Number(o.value) < 60).map(o => o.textContent).join("|")`).Str()
	if short != "" {
		t.Fatalf("offered below the hourly minimum: %s", short)
	}
	assertSays(t, "the choices", page.MustEval(`() => document.querySelector("#sourcesList select").textContent`).Str(), "every hour")
}

// Rating: one short line and a way to the profile; the model editor waits
// under Advanced and still saves.
func TestRatingWindowHidesModels(t *testing.T) {
	ctx := context.Background()
	page, svc, _ := settingsBoard(t, nil)
	openModal(t, page, "ratingBtn", "ratingModalOverlay")
	text := onScreen(page, "#ratingModalOverlay")
	assertSays(t, "the window", text, "grade from A to F", "Advanced", "Edit my profile")
	assertNotSays(t, "the window", text, "A/B", "model", "MCP", "Challenger", "rater")
	assertFitsPhone(t, page, "ratingModalOverlay")

	openRatingAdvanced(page)
	page.MustElement("#defaultModelInput").MustSelectAllText().MustInput("my-rater")
	clickID(page, "saveRatingConfigBtn")
	waitUntil(t, func() bool {
		c, _ := svc.RatingConfig(ctx)
		return c.DefaultModel == "my-rater"
	}, "the model wasn't saved")

	// Advanced closes again next time, and the button goes to the profile.
	page.MustWait(`() => !document.getElementById("ratingModalOverlay").classList.contains("open")`)
	openModal(t, page, "ratingBtn", "ratingModalOverlay")
	assertNotSays(t, "the window", onScreen(page, "#ratingModalOverlay"), "my-rater", "Challenger")
	clickID(page, "ratingProfileBtn")
	page.MustWait(`() => document.getElementById("profileModalOverlay").classList.contains("open") && !document.getElementById("ratingModalOverlay").classList.contains("open")`)
}

// My CV: one line, a button or a drop, no second click and never a path.
// Saying no to replacing About me leaves the profile alone.
func TestMyCVWindowUploadsAtOnce(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	page, _, dir := settingsBoard(t, nil)
	files := t.TempDir()
	first := filepath.Join(files, "first.md")
	os.WriteFile(first, []byte("# Nguyễn Văn Khang\n\nMarketing AI, 3 năm kinh nghiệm với n8n tại Vịt Vàng."), 0o600)
	bad := filepath.Join(files, "notes.txt")
	os.WriteFile(bad, []byte("plain notes"), 0o600)
	profile := func() string {
		b, _ := os.ReadFile(filepath.Join(dir, "profile.md"))
		return string(b)
	}

	openModal(t, page, "myCVBtn", "cvModalOverlay")
	page.MustElementR("#cvStatus", "Add your CV")
	text := onScreen(page, "#cvModalOverlay")
	assertNotSays(t, "the window", text, "profile.md", dir, "/tmp", "/home")
	if lines := strings.Split(strings.TrimSpace(onScreen(page, "#cvStatus")), "\n"); len(lines) != 1 {
		t.Fatalf("status is %d lines: %q", len(lines), lines)
	}
	if page.MustHas("#cvUploadBtn") {
		t.Fatal("a second Upload button is still there")
	}
	assertFitsPhone(t, page, "cvModalOverlay")

	// The button is the keyboard's way to the file picker.
	page.MustEval(`() => { window.__picked = false; document.getElementById("cvFileInput").click = () => { window.__picked = true; }; }`)
	clickID(page, "cvChooseBtn")
	if !page.MustEval(`() => window.__picked`).Bool() {
		t.Fatal("Choose file doesn't open the file picker")
	}

	// A file it can't read says why, in the window.
	page.MustElement("#cvFileInput").MustSetFiles(bad)
	waitVisible(page, "#cvResult")
	page.MustElement("#cvFileInput").MustSetFiles(first)
	page.MustElementR("#cvStatus", "first.md, added just now")
	page.MustWait(`() => document.getElementById("cvResult").hidden`)
	if !strings.Contains(profile(), "Vịt Vàng") {
		t.Fatalf("profile after the upload:\n%s", profile())
	}
	assertNotSays(t, "the window", onScreen(page, "#cvModalOverlay"), "profile.md", dir)
	assertSays(t, "the button", onScreen(page, "#cvChooseBtn"), "Replace it")

	// A drop uploads too, after the same question; No keeps the profile.
	drop := func(text string) {
		go func() {
			defer func() { _ = recover() }() // the dialog holds this call; see TestLoginsPanel
			page.MustEval(`text => {
				const dt = new DataTransfer();
				dt.items.add(new File([text], "dropped.md"));
				const o = document.getElementById("cvModalOverlay");
				for (const type of ["dragover", "drop"]) o.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt }));
			}`, text)
		}()
	}
	wait, handle := page.MustHandleDialog()
	drop("# Nguyễn Văn Khang\n\nData analyst, 5 năm kinh nghiệm SQL và Python tại Duck Pay.")
	if d := wait(); !strings.Contains(d.Message, "About me") {
		t.Fatalf("dialog = %q", d.Message)
	}
	handle(false, "")
	page.MustWait(`() => !document.getElementById("cvChooseBtn").disabled`)
	if strings.Contains(profile(), "Duck Pay") {
		t.Fatal("said no, and About me was replaced anyway")
	}

	wait, handle = page.MustHandleDialog()
	drop("# Nguyễn Văn Khang\n\nData analyst, 5 năm kinh nghiệm SQL và Python tại Duck Pay.")
	wait()
	handle(true, "")
	page.MustElementR("#cvStatus", "dropped.md")
	if !strings.Contains(profile(), "Duck Pay") {
		t.Fatalf("profile after replacing:\n%s", profile())
	}
}
