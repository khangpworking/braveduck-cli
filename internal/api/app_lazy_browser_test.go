//go:build browser

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// Cold openings must fetch their code from the embedded board, display the
// real content, and close without leaving a dialog or JavaScript error behind.
func TestAppLazyWindows(t *testing.T) {
	list := appJobs()
	list[0].CVStatus = "ready"
	st, svc, _ := appBoard(t, list)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	svc.SetHosted(service.HostedInfo{User: "tester"})
	if err := os.WriteFile(filepath.Join(dir, "profile.md"), []byte("Accountant, 3 years of experience."), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := Handler(st, svc, testToken)
	settingsReady := make(chan struct{})
	var releaseOnce sync.Once
	releaseSettings := func() { releaseOnce.Do(func() { close(settingsReady) }) }
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/SettingsContent-") {
			<-settingsReady
		}
		// Exercise the apply dialog without a saved login or an external site.
		switch {
		case r.URL.Path == "/api/platforms":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]service.PlatformStatus{{ID: "topcv", Name: "TopCV", Status: "connected", Domains: []string{"topcv.vn"}}})
		case strings.HasSuffix(r.URL.Path, "/apply") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(service.ApplyPrep{Site: "TopCV", SiteID: "topcv", CVPDF: "/tmp/test-cv.pdf"})
		default:
			handler.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	t.Cleanup(releaseSettings)
	page := launchBrowser(t).MustPage("")
	page.MustEvalOnNewDocument(`window.__errors = [];
		addEventListener('error', e => window.__errors.push(e.message));
		addEventListener('unhandledrejection', e => window.__errors.push(String(e.reason)));
		const error = console.error;
		console.error = (...args) => { window.__errors.push(args.join(' ')); error.apply(console, args) }`)
	page.MustSetViewport(1280, 844, 1, false)
	page.MustNavigate(ts.URL + BoardPath).MustWaitLoad()
	page.MustElement("section[aria-label='Accountant']")
	loaded := func(name string) bool {
		return page.MustEval(`name => performance.getEntriesByType('resource').some(r => new URL(r.name).pathname.includes('/assets/' + name + '-'))`, name).Bool()
	}
	chunks := []string{"SettingsContent", "ProfileWindow", "CVWindow", "KeywordsWindow", "SourcesWindow", "LocationWindow", "RatingWindow", "LoginsWindow", "FacebookWindow", "ActivityWindow", "AgentWindow", "QuickReview", "AddJob", "AskAI", "ApplySteps", "EditDetails"}
	for _, name := range chunks {
		if loaded(name) {
			t.Errorf("%s loaded before its window was opened", name)
		}
	}
	// A Get started link also has to work before the Settings sheet loads.
	boardBounds := page.MustEval(`() => JSON.stringify(document.querySelector('[data-job]').getBoundingClientRect())`).Str()
	tap(page, "[data-get-started] button", "Upload your CV")
	page.MustElement("[role=dialog][aria-busy]")
	if bounds := page.MustEval(`() => JSON.stringify(document.querySelector('[data-job]').getBoundingClientRect())`).Str(); bounds != boardBounds {
		t.Errorf("loading a window moved the board: %s -> %s", boardBounds, bounds)
	}
	closeWin(page)
	releaseSettings()
	page.MustWait(`() => performance.getEntriesByType('resource').some(r => r.name.includes('/assets/SettingsContent-'))`)
	if page.MustHas("[role=dialog]") {
		t.Fatal("a window cancelled during loading reopened")
	}
	tap(page, "[data-get-started] button", "Upload your CV")
	page.MustElementR("[role=dialog] button", "Choose file")
	closeWin(page)
	for _, window := range []struct{ row, title, content string }{
		{"^Your profile$", "Your profile", "What job do you want?"},
		{"^Search terms$", "Search terms", "What jobs should we look for?"},
		{"^Job sources$", "Job sources", "We look for jobs on these sites."},
		{"^Your location$", "Your location", "Use my current location"},
		{"^Rating$", "How jobs get rated", "grade from A to F"},
		{"^Job-site logins$", "Job-site logins", "TopCV"},
		{"^Facebook groups$", "Facebook groups you follow", "You don't follow any Facebook groups yet."},
		{"^Recent activity$", "Recent activity", "Nothing yet."},
		{"^Connect your AI agent$", "Connect your AI agent", "Make a key for your agent"},
		{"^Language", "Language", "Tiếng Việt"},
	} {
		d := openWin(t, page, window.row, window.title)
		waitText(t, d, window.content)
		closeWin(page)
	}
	tap(page, "button", "Go through them one by one")
	page.MustElement("[data-review-job]")
	closeWin(page)
	tap(page, "button", "Add job")
	page.MustElement(`textarea[aria-label="Link or text of the posting"]`)
	closeWin(page)
	openMoreMenu(page)
	tap(page, "[role=menuitem]", "Edit details")
	page.MustElementR("[role=dialog] label", "Title").MustElement("input")
	closeWin(page)
	tap(page, "button", "Rate it again")
	page.MustElementR("[role=dialog] pre", "Accountant")
	closeWin(page)
	tap(page, "button", "Apply on TopCV")
	page.MustElementR("[role=dialog] li", "Upload this CV: /tmp/test-cv.pdf")
	closeWin(page)
	for _, name := range chunks {
		if !loaded(name) {
			t.Errorf("%s wasn't fetched when opened", name)
		}
	}
	if errors := page.MustEval(`() => window.__errors.join('\n')`).Str(); errors != "" {
		t.Errorf("JavaScript errors:\n%s", errors)
	}
	if remote := page.MustEval(`() => performance.getEntriesByType('resource').filter(r => /\.js(?:\?|$)/.test(r.name) && new URL(r.name).origin !== location.origin).map(r => r.name).join('\n')`).Str(); remote != "" {
		t.Errorf("JavaScript loaded outside the board:\n%s", remote)
	}
}
