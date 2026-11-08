//go:build browser

package api

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The new board's settings: the Settings panel, every window it opens, and
// Get started. Helpers come from app_browser_test.go (appBoard, appJobs) and
// board_browser_test.go (launchBrowser, waitUntil).

// settingsApp opens the new board at width over list, after setup has
// configured the service (geocoder, crawlers, hosted, …).
func settingsApp(t *testing.T, list []jobs.Job, width int, setup func(svc *service.Service)) (*rod.Page, *service.Service) {
	t.Helper()
	_, svc, url := appBoard(t, list)
	if setup != nil {
		setup(svc)
	}
	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(width, 844, 1, width < 760)
	page.MustNavigate(url).MustWaitLoad()
	if width < 760 {
		page.MustElement("header")
	} else {
		page.MustElement("nav")
	}
	return page, svc
}

// tap clicks the element under from whose text matches re (a script click:
// see CONTRIBUTING.md on why not a pointer click).
func tap(from interface {
	MustElementR(string, string) *rod.Element
}, selector, re string) {
	from.MustElementR(selector, re).MustEval(`() => this.click()`)
}

// openSettings opens the Settings panel: the menu item on a computer, the
// gear in the header on a phone.
func openSettings(page *rod.Page, label string) *rod.Element {
	if page.MustEval(`() => innerWidth < 760`).Bool() {
		page.MustElement(`header button[aria-label="` + label + `"]`).MustEval(`() => this.click()`)
	} else {
		tap(page, "nav button", "^"+label+"$")
	}
	return page.MustElement(`[role=dialog] nav[aria-label="` + label + `"]`)
}

// openWin picks row in the Settings panel and returns the window it opens,
// once its title reads title.
func openWin(t *testing.T, page *rod.Page, row, title string) *rod.Element {
	t.Helper()
	tap(openSettings(page, "Settings"), "button", row)
	return winTitled(page, title)
}

func winTitled(page *rod.Page, title string) *rod.Element {
	page.MustWait(`() => document.querySelectorAll("[role=dialog]").length === 1 && !document.querySelector("[role=dialog] nav, [role=dialog][aria-busy]")`)
	page.MustElementR("[role=dialog] h2", "^"+title+"$")
	return page.MustElement("[role=dialog]")
}

func closeWin(page *rod.Page) {
	page.Keyboard.MustType(input.Escape)
	page.MustWait(`() => !document.querySelector("[role=dialog]")`)
}

func waitText(t *testing.T, el *rod.Element, want string) {
	t.Helper()
	waitUntil(t, func() bool { return strings.Contains(el.MustText(), want) }, "never says "+want+":\n"+el.MustText())
}

func toastSays(page *rod.Page, re string) { page.MustElementR("[data-sonner-toast]", re) }

// Every row opens its window; the shared-server item isn't there on your own
// computer; another part of the board can open a window by name.
func TestAppSettingsWindows(t *testing.T) {
	page, _ := settingsApp(t, appJobs(), 1280, nil)

	if txt := openSettings(page, "Settings").MustText(); strings.Contains(txt, "Connect your AI agent") {
		t.Errorf("the agent item shows on your own computer:\n%s", txt)
	}
	closeWin(page)

	for _, w := range []struct{ row, title, says string }{
		{"^Your profile$", "Your profile", "What job do you want?"},
		{"^My CV$", "My CV", "Choose file"},
		{"^Search terms$", "Search terms", "What jobs should we look for?"},
		{"^Job sources$", "Job sources", "We look for jobs on these sites."},
		{"^Your location$", "Your location", "Use my current location"},
		{"^Rating$", "How jobs get rated", "grade from A to F"},
		{"^Job-site logins$", "Job-site logins", "TopCV"},
		{"^Facebook groups$", "Facebook groups you follow", "You don't follow any Facebook groups yet."},
		{"^Recent activity$", "Recent activity", "Nothing yet."},
		{"^Language", "Language", "Tiếng Việt"},
	} {
		d := openWin(t, page, w.row, w.title)
		waitText(t, d, w.says)
		closeWin(page)
	}

	// The logins list: one row per site, each with Connect (not pressed here:
	// it opens a real browser window).
	d := openWin(t, page, "^Job-site logins$", "Job-site logins")
	page.MustElement(`[role=dialog] [data-platform="topcv"]`)
	if n := d.MustEval(`() => [...this.querySelectorAll("[data-platform] button")].filter(b => b.textContent === "Connect").length`).Int(); n < 4 {
		t.Errorf("want Connect on every site, got %d", n)
	}
	closeWin(page)

	// The job detail opens a window by name (rating without a profile).
	page.MustEval(`() => window.dispatchEvent(new CustomEvent("braveduck:open", { detail: "profile" }))`)
	winTitled(page, "Your profile")
}

// Search terms: tags with a live count, filters under More filters, and Save
// sends every list (a term typed without Enter included).
func TestAppSearchTerms(t *testing.T) {
	ctx := context.Background()
	page, svc := settingsApp(t, appJobs(), 1280, nil)
	d := openWin(t, page, "^Search terms$", "Search terms")

	page.MustElement("[role=dialog] input[data-tags-input]").MustInput("accountant")
	page.Keyboard.MustType(input.Enter)
	page.MustElementR("[role=dialog] [data-count]", "^3 jobs on your board mention these.$")
	page.MustElement("[role=dialog] input[data-tags-input]").MustInput("n8n")

	tap(page, "[role=dialog] button", "^More filters$")
	waitUntil(t, func() bool { return len(d.MustElements("input[data-tags-input]")) == 5 }, "More filters didn't open")
	d.MustElements("input[data-tags-input]")[1].MustInput("Sales, Telesales")
	tap(page, "[role=dialog] button", "^Save$")

	waitUntil(t, func() bool {
		k := must(svc.Keywords(ctx))
		return slices.Equal(k["search_queries"], []string{"accountant", "n8n"}) && slices.Equal(k["negative_content"], []string{"Sales", "Telesales"})
	}, "the search terms weren't saved")
	toastSays(page, "Search terms saved")

	d = openWin(t, page, "^Search terms$", "Search terms")
	page.MustElement(`[role=dialog] [aria-label="Remove n8n"]`)
	page.MustElementR("[role=dialog] button", `^More filters \(2 in use\)$`)
}

// The profile form saves tags, a choice and pay, and the text mode shows
// what was saved.
func TestAppProfileForm(t *testing.T) {
	ctx := context.Background()
	page, svc := settingsApp(t, appJobs(), 1280, nil)
	openWin(t, page, "^Your profile$", "Your profile")
	page.MustElementR("[role=dialog] [role=status]", "Not filled in yet")

	page.MustElement("[role=dialog] input[data-tags-input]").MustInput("Kế toán")
	page.Keyboard.MustType(input.Enter)
	tap(page, `[role=dialog] [role=group][aria-label="Level"] button`, "^Senior$")
	amount := page.MustElement(`[role=dialog] input[aria-label="The least you'd accept amount"]`)
	amount.MustInput("15000000")
	waitUntil(t, func() bool { return amount.MustProperty("value").Str() == "15,000,000" }, "the amount isn't grouped")
	tap(page, "[role=dialog] button", "^Save$")

	waitUntil(t, func() bool {
		f, _, _ := svc.ProfileFormNow(ctx)
		return slices.Contains(f.Roles, "Kế toán") && slices.Contains(f.Seniority, "Senior") && f.MinPay.Amount == 15000000
	}, "the profile form wasn't saved")
	toastSays(page, "Profile saved")
	page.MustElementR("[role=dialog] [role=status]", "Filled in")

	tap(page, "[role=dialog] button", "^Edit as text instead$")
	text := page.MustElement("[role=dialog] #profile-text")
	waitUntil(t, func() bool { return strings.Contains(text.MustProperty("value").Str(), "Kế toán") }, "the text mode doesn't show the saved form")
}

// My CV uploads at once; replacing an About me already there asks first,
// and Cancel keeps it.
func TestAppCVUpload(t *testing.T) {
	ctx := context.Background()
	page, svc := settingsApp(t, appJobs(), 1280, nil)
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.md"), filepath.Join(dir, "second.md")
	os.WriteFile(first, []byte("# Nguyễn Văn Khang\n\nMarketing AI, 3 năm kinh nghiệm với n8n tại Vịt Vàng."), 0o600)
	os.WriteFile(second, []byte("# Nguyễn Văn Khang\n\nKế toán tổng hợp, 5 năm ở Công ty Hoa Sen."), 0o600)
	profile := func() string { return must(svc.Profile(ctx)).Text }

	openWin(t, page, "^My CV$", "My CV")
	page.MustElementR("[role=dialog] [data-cv-status]", "Add your CV")
	page.MustElement("[role=dialog] input[type=file]").MustSetFiles(first)
	page.MustElementR("[role=dialog] [data-cv-status]", "first.md, added just now")
	waitUntil(t, func() bool { return strings.Contains(profile(), "Vịt Vàng") }, "the CV's text isn't in the profile")
	toastSays(page, "CV added")
	page.MustElementR("[role=dialog] button", "^Replace it$")

	page.MustElement("[role=dialog] input[type=file]").MustSetFiles(second)
	page.MustElementR("[role=alertdialog]", `already has text`)
	tap(page, "[role=alertdialog] button", "^Cancel$")
	page.MustWait(`() => !document.querySelector("[role=alertdialog]")`)
	if p := profile(); strings.Contains(p, "Hoa Sen") || !strings.Contains(p, "Vịt Vàng") {
		t.Fatalf("Cancel changed the profile:\n%s", p)
	}

	page.MustElement("[role=dialog] input[type=file]").MustSetFiles(second)
	tap(page, "[role=alertdialog] button", "^Replace it$")
	waitUntil(t, func() bool { return strings.Contains(profile(), "Hoa Sen") }, "Replace didn't put the new CV in the profile")
	page.MustElementR("[role=dialog] [data-cv-status]", "second.md, added just now")
}

// Location: an address search (fake geocoder), the pin on the lazily loaded
// map, Save, and Forget after a question.
func TestAppLocation(t *testing.T) {
	ctx := context.Background()
	page, svc := settingsApp(t, appJobs(), 1280, func(svc *service.Service) { svc.SetGeocoder(boardFakeGeo{}) })
	openWin(t, page, "^Your location$", "Your location")
	page.MustElement("[role=dialog] .leaflet-container")
	page.MustElementR("[role=dialog] [data-picked]", "Click the map to place your pin.")

	search := page.MustElement(`[role=dialog] input[aria-label="Search an address"]`)
	search.MustInput("Nowhere at all")
	tap(page, "[role=dialog] button", "^Search$")
	page.MustElementR("[role=dialog] [data-places]", "No match. Try adding the city.")
	search.MustSelectAllText().MustInput("Landmark 81")
	tap(page, "[role=dialog] button", "^Search$")
	tap(page, "[role=dialog] [data-places] button", "Bình Thạnh")
	page.MustElementR("[role=dialog] [data-picked]", "Landmark 81")
	page.MustElement("[role=dialog] .leaflet-marker-icon")
	page.MustElement("[role=dialog] #max-km").MustSelectAllText().MustInput("8")
	tap(page, "[role=dialog] button", "^Save$")

	waitUntil(t, func() bool {
		l := must(svc.GetLocation(ctx))
		return l.Set && l.MaxKM == 8 && l.Lat == 10.7949 && l.Label == "Landmark 81, Bình Thạnh"
	}, "the location wasn't saved")
	toastSays(page, "Location saved")

	openWin(t, page, "^Your location$", "Your location")
	waitText(t, page.MustElement("[role=dialog] [data-picked]"), "Saved: Landmark 81")
	tap(page, "[role=dialog] button", "^Forget my location$")
	tap(page, "[role=alertdialog] button", "^Forget my location$")
	waitUntil(t, func() bool { return !must(svc.GetLocation(ctx)).Set }, "Forget didn't clear the location")
}

// Job sources: names in words, a schedule change saved, Check now explained
// until there are search terms, then checking the sites that are on; a
// failing site gets a warning with Try again.
func TestAppSources(t *testing.T) {
	ctx := context.Background()
	vnw, cl := &countingCrawler{id: "vietnamworks"}, &countingCrawler{id: "careerlink"}
	page, svc := settingsApp(t, appJobs(), 1280, func(svc *service.Service) {
		svc.SetCrawlers(vnw, cl)
		svc.SetCrawlSchedule(ctx, "vietnamworks", 120)
		svc.SetCrawlSchedule(ctx, "careerlink", 0)
	})
	openWin(t, page, "^Job sources$", "Job sources")
	waitText(t, page.MustElement(`[role=dialog] [data-source="vietnamworks"]`), "VietnamWorks")
	waitText(t, page.MustElement(`[role=dialog] [data-source="careerlink"]`), "Not checked yet")
	check := page.MustElementR("[role=dialog] button", "^Check now$")
	if !check.MustProperty("disabled").Bool() {
		t.Fatal("Check now works with no search terms")
	}
	page.MustElementR("[role=dialog] [data-hint]", "First tell us what jobs to look for.")

	page.MustElement(`[role=dialog] select[aria-label^="CareerLink"]`).MustSelect("every hour")
	every := func(source string) int {
		for _, sc := range must(svc.CrawlSchedules(ctx)) {
			if sc.Source == source {
				return sc.EveryMinutes
			}
		}
		return -1
	}
	waitUntil(t, func() bool { return every("careerlink") == 60 }, "the schedule wasn't saved")
	toastSays(page, "CareerLink: every hour")

	// Search terms arrive (from anywhere): Check now checks both sites.
	svc.SetKeywords(ctx, keyword.Sets{"search_queries": {"n8n"}})
	page.MustWait(`() => { const b = [...document.querySelectorAll("[role=dialog] button")].find(b => b.textContent.trim() === "Check now"); return b && !b.disabled }`)
	tap(page, "[role=dialog] button", "^Check now$")
	toastSays(page, "Found 2 new jobs.")
	if vnw.calls.Load() == 0 || cl.calls.Load() == 0 {
		t.Fatalf("Check now didn't check every site that's on: vietnamworks %d, careerlink %d", vnw.calls.Load(), cl.calls.Load())
	}

	// A failed check reads as words, and the warning offers Try again.
	svc.ReportCrawl(ctx, "careerlink", false, "HTTP 503 from upstream")
	page.MustElementR(`[role=dialog] [data-source="careerlink"]`, "The last check didn't work")
	if txt := page.MustElement(`[role=dialog] [data-source="careerlink"]`).MustText(); strings.Contains(txt, "HTTP 503") {
		t.Errorf("the site row shows the raw error: %q", txt)
	}
	page.MustElementR("[role=dialog] [data-warnings]", "CareerLink couldn't check for jobs")
	page.MustElementR("[role=dialog] [data-warnings] button", "^Try again$")

	tap(page, "[role=dialog] button", "^Advanced$")
	page.MustElementR("[role=dialog] [data-advanced]", "Automatic job rating")
}

// Rating: the Jev notice and its opt-out, saved at once; the raters still
// save under Advanced.
func TestAppRatingJevOptOut(t *testing.T) {
	ctx := context.Background()
	page, svc := settingsApp(t, appJobs(), 1280, func(svc *service.Service) { svc.SetJev(apiFakeJev{}) })
	openWin(t, page, "^Rating$", "How jobs get rated")
	page.MustElementR("[role=dialog] p", "sent to TypeSafe's Jev")
	page.MustElement(`[role=dialog] [role=checkbox]`).MustEval(`() => this.click()`)
	waitUntil(t, func() bool { return must(svc.JevOptedOut(ctx)) }, "the opt-out wasn't saved")
	page.MustWait(`() => ![...document.querySelectorAll("[role=dialog] p")].some(p => p.textContent.includes("TypeSafe's Jev"))`)

	tap(page, "[role=dialog] button", "^Advanced$")
	page.MustElement("[role=dialog] #rater-default").MustSelectAllText().MustInput("my-rater")
	tap(page, "[role=dialog] [data-advanced] button", "^Save$")
	waitUntil(t, func() bool { return must(svc.RatingConfig(ctx)).DefaultModel == "my-rater" }, "the rater wasn't saved")
}

// On a shared server: who is logged in and Log out, the agent key window,
// no Connect on job sites, and nothing more often than hourly.
func TestAppHosted(t *testing.T) {
	page, _ := settingsApp(t, appJobs(), 1280, func(svc *service.Service) {
		svc.SetHosted(service.HostedInfo{User: "alice<b>", LogoutURL: "https://auth.example.com/logout"})
		svc.SetCrawlers(&countingCrawler{id: "vietnamworks"})
	})
	sheet := openSettings(page, "Settings").MustParent()
	waitText(t, sheet, "Logged in as alice<b>")
	if href := page.MustElementR("[role=dialog] a", "^Log out$").MustAttribute("href"); href == nil || *href != "https://auth.example.com/logout" {
		t.Errorf("Log out goes to %v", href)
	}
	tap(page, "[role=dialog] nav button", "^Connect your AI agent$")
	d := winTitled(page, "Connect your AI agent")
	tap(page, "[role=dialog] button", "^Make a key for your agent$")
	cmd := page.MustElement("[role=dialog] [data-command]").MustText()
	if !strings.Contains(cmd, "/mcp") || !strings.Contains(cmd, "Authorization: Bearer ") {
		t.Errorf("the connect command is %q", cmd)
	}
	page.MustElementR("[role=dialog] button", "^Remove the key$")
	closeWin(page)

	d = openWin(t, page, "^Job-site logins$", "Job-site logins")
	waitText(t, d, "it's off on this shared server")
	page.MustElement(`[role=dialog] [data-platform="topcv"]`)
	if d.MustHas("[data-platform] button") {
		t.Error("a shared server offers buttons for logins that aren't saved")
	}
	closeWin(page)

	d = openWin(t, page, "^Job sources$", "Job sources")
	page.MustElement(`[role=dialog] [data-source="vietnamworks"] option`)
	short := d.MustEval(`() => [...this.querySelectorAll('[data-source="vietnamworks"] option')].filter(o => Number(o.value) > 0 && Number(o.value) < 60).length`).Int()
	if short != 0 {
		t.Errorf("a shared server offers %d intervals under an hour", short)
	}
}

// Get started lists only the steps left; each opens its window.
func TestAppGetStarted(t *testing.T) {
	page, svc := settingsApp(t, nil, 1280, nil)
	card := page.MustElement("[data-get-started]")
	waitText(t, card, "3 steps left")
	for _, step := range []string{"Upload your CV", "Say what you're looking for", "Add search terms"} {
		waitText(t, card, step)
	}
	svc.SetKeywords(context.Background(), keyword.Sets{"search_queries": {"n8n"}})
	waitText(t, card, "2 steps left")
	if strings.Contains(card.MustText(), "Add search terms") {
		t.Error("a done step is still listed")
	}
	tap(page, "[data-get-started] button", "Say what you're looking for")
	winTitled(page, "Your profile")
}

// In Vietnamese on a phone: the gear opens Settings, rows and windows read
// in Vietnamese (activity lines too), and nothing is wider than the screen.
func TestAppSettingsVietnamese(t *testing.T) {
	t.Setenv("BRAVEDUCK_LANG", "vi")
	page, _ := settingsApp(t, appJobs(), 390, func(svc *service.Service) {
		svc.AddJob(context.Background(), service.NewJob{Title: "Kế toán kho"})
	})
	page.MustElementR("[data-get-started]", "Bắt đầu")
	nav := openSettings(page, "Cài đặt")
	for _, want := range []string{"Hồ sơ của bạn", "CV của bạn", "Từ khóa tìm việc", "Nguồn tìm việc", "Vị trí của bạn", "Hoạt động gần đây", "Ngôn ngữ"} {
		waitText(t, nav, want)
	}
	tap(nav, "button", "^Hoạt động gần đây$")
	d := winTitled(page, "Hoạt động gần đây")
	page.MustElementR("[role=dialog] [data-activity]", `Đã thêm "Kế toán kho"`)
	closeWin(page)

	tap(openSettings(page, "Cài đặt"), "button", "^Từ khóa tìm việc$")
	d = winTitled(page, "Từ khóa tìm việc")
	waitText(t, d, "Bạn muốn tìm việc gì?")
	wide := page.MustEval(`() => [...document.querySelectorAll("[role=dialog] *")].filter(e => e.getBoundingClientRect().right > innerWidth + 1).length`).Int()
	if wide > 0 || page.MustEval(`() => document.documentElement.scrollWidth`).Int() > 390 {
		t.Errorf("%d things in the window are wider than the phone", wide)
	}
}

// Facebook groups: each followed group says whether it can be read; Unfollow
// drops it.
func TestAppFacebookGroups(t *testing.T) {
	ctx := context.Background()
	page, svc := settingsApp(t, appJobs(), 1280, func(svc *service.Service) {
		g, _, err := svc.FollowGroup(ctx, "https://www.facebook.com/groups/ketoanhcm")
		if err != nil {
			t.Fatal(err)
		}
		svc.SetGroupStatus(ctx, g.Key, "Kế toán HCM", "private", "")
	})
	openWin(t, page, "^Facebook groups$", "Facebook groups you follow")
	page.MustElementR("[role=dialog] [data-group]", "Kế toán HCM")
	page.MustElementR("[role=dialog] [data-group]", "Private — only members see its posts")
	tap(page, "[role=dialog] [data-group] button", "^Unfollow$")
	waitUntil(t, func() bool { return len(must(svc.FBGroups(ctx))) == 0 }, "Unfollow didn't drop the group")
	page.MustElementR("[role=dialog] p", "You don't follow any Facebook groups yet.")
}

// Download my jobs saves the CSV (fetched with the token, then saved from a
// Blob); great-match notifications pop for a job rated A after they're on;
// the language switch reloads the board in Vietnamese.
func TestAppExportNotifyLanguage(t *testing.T) {
	st, _, url := appBoard(t, appJobs())
	page := launchBrowser(t).MustPage("")
	// The browser's notifications, recorded instead of shown.
	page.MustEvalOnNewDocument(`window.__notes = []; window.Notification = class {
		static permission = "granted"
		static requestPermission() { return Promise.resolve("granted") }
		constructor(title, o) { window.__notes.push(title + " | " + o.body) }
		close() {}
	}`)
	page.MustSetViewport(1280, 844, 1, false)
	page.MustNavigate(url).MustWaitLoad()
	page.MustElement("[data-job]")

	page.MustEval(`() => { HTMLAnchorElement.prototype.click = function () {
		window.__file = this.download; fetch(this.href).then(r => r.text()).then(t => window.__csv = t) } }`)
	tap(openSettings(page, "Settings"), "button", `^Download my jobs \(CSV\)$`)
	page.MustWait(`() => typeof window.__csv === "string"`)
	if name, csv := page.MustEval(`() => window.__file`).Str(), page.MustEval(`() => window.__csv`).Str(); name != "braveduck-jobs.csv" || !strings.Contains(csv, "Tour accountant") {
		t.Fatalf("downloaded %q:\n%.200s", name, csv)
	}

	page.MustElement(`[role=dialog] [role=switch]`).MustEval(`() => this.click()`)
	toastSays(page, "You'll be notified of new A and B matches")
	closeWin(page)
	if n := page.MustEval(`() => window.__notes.length`).Int(); n != 0 {
		t.Fatalf("matches rated before turning it on notified: %v", page.MustEval(`() => window.__notes`).Arr())
	}
	if _, err := st.Add(context.Background(), jobs.Job{Title: "Payroll lead", Company: "Sen Co", Status: "saved",
		Score: 4.8, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	page.MustWait(`() => window.__notes.length > 0`)
	if note := page.MustEval(`() => window.__notes.join("\n")`).Str(); note != "Great match: Payroll lead | A · 4.8 — Sen Co" {
		t.Errorf("the notification reads %q", note)
	}

	tap(openSettings(page, "Settings"), "button", "^Language")
	tap(page, "[role=dialog] button", "^Tiếng Việt$")
	page.MustElementR("nav button", "^Cài đặt$")
}
