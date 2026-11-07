//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// appBoard serves the new board over a store holding list, and returns the
// store, the service and the board's URL.
func appBoard(t *testing.T, list []jobs.Job) (*store.Store, *service.Service, string) {
	t.Helper()
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	// A file, not :memory:, so the live updates can watch it.
	st, err := store.Open(filepath.Join(t.TempDir(), "bd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	now := time.Now()
	for _, j := range list {
		if _, err := st.Add(context.Background(), j, now); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	t.Cleanup(ts.Close)
	return st, svc, ts.URL + BoardPath
}

func appDay(d int) string { return time.Now().AddDate(0, 0, d).Format("2006-01-02") }

// appJobs is a board in its third week: rated saved jobs, one closing soon,
// and two applications, one of them waiting for an answer for 8 days.
func appJobs() []jobs.Job {
	return []jobs.Job{
		{Title: "Accountant", Company: "Công ty TNHH Hoa Sen", Status: "saved", Score: 4.7, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored",
			ScoreReasoning: "Same field • Pays more than you want", ExpiresAt: "expire:" + appDay(2), URL: "https://www.topcv.vn/viec-lam/a/1.html",
			Salary: "14000000 - 18000000 VND/month", DiscoverySource: "topcv"},
		{Title: "Cashier", Company: "Mộc Lan", Status: "saved", Score: 3.6, HasScore: true, ScoreLetter: "C", ScoreStatus: "scored",
			FullJD: "Phải làm thứ 7", URL: "https://www.topcv.vn/viec-lam/b/2.html"},
		{Title: "Unrated clerk", Status: "saved"},
		{Title: "Tour accountant", Company: "Sen Việt", Status: "applied", ExpiresAt: "applied:" + appDay(-8)},
		{Title: "Warehouse accountant", Status: "applied", ExpiresAt: "applied:" + appDay(-3)},
	}
}

func appPage(t *testing.T, url string, width int) *rod.Page {
	t.Helper()
	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(width, 844, 1, width < 760)
	page.MustNavigate(url).MustWaitLoad()
	page.MustElement("[data-job]")
	return page
}

func jobStatus(t *testing.T, st *store.Store, title string) string {
	t.Helper()
	list, err := st.List(context.Background(), service.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range list {
		if j.Title == title {
			return j.Status
		}
	}
	t.Fatalf("no job %q", title)
	return ""
}

// The list shows the best rated first with its deal-breaker in words, Today
// puts the week-old application first, and the detail shows what to do next.
func TestAppListTodayAndDetail(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)

	rows := page.MustEval(`() => [...document.querySelectorAll("[data-job]")].map(e => e.innerText.split("\n")[1]).join("|")`).String()
	if !strings.HasPrefix(rows, "Accountant|Cashier|Unrated clerk") {
		t.Errorf("saved jobs aren't best rated first: %s", rows)
	}
	if txt := page.MustElementR("[data-job]", "Cashier").MustText(); !strings.Contains(txt, "Works on Saturdays") {
		t.Errorf("the deal-breaker isn't on the row: %q", txt)
	}
	if txt := page.MustElementR("[data-job]", "Accountant").MustText(); !strings.Contains(txt, "14–18 triệu/tháng") {
		t.Errorf("pay isn't in triệu: %q", txt)
	}

	today := page.MustElement(`[aria-label="Today"]`).MustText()
	if i, j := strings.Index(today, "Ask Sen Việt for an answer"), strings.Index(today, "2 days left to apply"); i < 0 || j < 0 || i > j {
		t.Errorf("Today should show the follow-up, then the deadline:\n%s", today)
	}
	if strings.Contains(today, "Warehouse accountant") {
		t.Errorf("Today shows an application only 3 days old:\n%s", today)
	}

	detail := page.MustElement("section[aria-label='Accountant']").MustText()
	for _, want := range []string{"2 days left to apply", "Great fit", "Same field", "Open the posting to apply"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail is missing %q:\n%s", want, detail)
		}
	}

	page.MustElementR("button", "Ask Sen Việt").MustEval(`() => this.click()`)
	page.MustElementR("section[aria-label='Tour accountant']", "Ask the company for an answer")
	page.MustElementR("button", "Copy message")
	page.MustElementR("button", "The company called me for an interview")
}

// Applying is the user's: opening the posting asks whether they sent a CV,
// and only "Yes" moves the job. Skipping moves it to Closed, with Undo.
func TestAppApplyAndSkip(t *testing.T) {
	st, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)

	// The link opens a new tab; keep the test on this one.
	page.MustEval(`() => { window.open = () => null; document.querySelectorAll("a[target=_blank]").forEach(a => a.addEventListener("click", e => e.preventDefault())) }`)
	page.MustElementR("a", "Open the posting to apply").MustEval(`() => this.click()`)
	page.MustElementR("p", "Did you send your CV?")
	if s := jobStatus(t, st, "Accountant"); s != "saved" {
		t.Fatalf("opening the posting moved the job to %s", s)
	}
	page.MustElementR("button", "Yes, I applied").MustEval(`() => this.click()`)
	waitUntil(t, func() bool { return jobStatus(t, st, "Accountant") == "applied" }, "Yes didn't move the job to Applied")

	// The next saved job is shown; skip it, then undo.
	page.MustElement("section[aria-label='Cashier']")
	page.MustElementR("button", "Not a fit, skip it").MustEval(`() => this.click()`)
	waitUntil(t, func() bool { return jobStatus(t, st, "Cashier") == "closed" }, "skipping didn't close the job")
	// The Undo of this move: the toast from applying may still be showing,
	// with an Undo of its own.
	page.MustElementR("[data-sonner-toast]", "Moved to Closed").MustElementR("button", "Undo").MustEval(`() => this.click()`)
	waitUntil(t, func() bool { return jobStatus(t, st, "Cashier") == "saved" }, "Undo didn't bring the job back")
}

// Pasting a job adds it and opens it; pasting it again names the job it
// matches instead of adding a second one.
func TestAppAddJob(t *testing.T) {
	st, _, url := appBoard(t, nil)
	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(1280, 844, 1, false)
	page.MustNavigate(url).MustWaitLoad()

	text := "Cần tuyển kế toán kho, làm tại Quận 5, lương 10-12 triệu. Gửi CV qua email hr@example.com."
	for round := range 2 {
		page.MustElementR("button", "Add job").MustEval(`() => this.click()`)
		box := page.MustElement(`textarea[aria-label="Link or text of the posting"]`)
		box.MustInput(text)
		page.MustElementR("[role=status]", "The text of a posting: ready to add")
		page.MustElementR("[role=dialog] button", "^Add$").MustEval(`() => this.click()`)
		if round == 0 {
			page.MustWait(`() => !document.querySelector("[role=dialog]")`)
			page.MustElement("[data-job]")
		} else {
			page.MustElementR("[role=alert]", "already on your board")
		}
	}
	list, err := st.List(context.Background(), service.ListFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("want exactly 1 job, got %d (%v)", len(list), err)
	}
}

// A change made anywhere else (here: the CLI's service) shows without a reload.
func TestAppLiveUpdates(t *testing.T) {
	_, svc, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	page.MustEval(`() => { window.notReloaded = true }`)
	list, err := svc.ListJobs(context.Background(), service.ListFilter{Status: "saved"})
	if err != nil {
		t.Fatal(err)
	}
	title := "Renamed by the CLI"
	if _, err := svc.UpdateJob(context.Background(), list[0].ID, service.Patch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	page.MustElementR("[data-job]", title)
	if !page.MustEval(`() => window.notReloaded === true`).Bool() {
		t.Fatal("the page reloaded")
	}
}

// On a phone the list and the job are separate screens, and the phone's own
// Back button returns from the job to the list.
func TestAppPhone(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 390)

	if page.MustHas("section[aria-label='Accountant']") {
		t.Fatal("a phone shows the job beside the list")
	}
	if n := page.MustEval(`() => document.querySelectorAll('[aria-label="Today"] button[type=button]').length`).Int(); n > 2 {
		t.Errorf("Today shows %d buttons on a phone; want one item and See all", n)
	}
	if w := page.MustEval(`() => document.documentElement.scrollWidth`).Int(); w > 390 {
		t.Errorf("the page scrolls sideways: %d px wide", w)
	}
	page.MustElementR("[data-job]", "Accountant").MustEval(`() => this.click()`)
	page.MustElement("section[aria-label='Accountant']")
	page.MustNavigateBack()
	page.MustWait(`() => !document.querySelector("section[aria-label='Accountant']")`)
	page.MustElementR("[data-job]", "Cashier")
}

// Vietnamese is the default language.
func TestAppVietnamese(t *testing.T) {
	t.Setenv("BRAVEDUCK_LANG", "vi")
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	nav := page.MustElement("nav").MustText()
	for _, want := range []string{"Thêm việc", "Hôm nay", "Đã lưu", "Đã nộp CV"} {
		if !strings.Contains(nav, want) {
			t.Errorf("the menu is missing %q:\n%s", want, nav)
		}
	}
	page.MustElementR("[data-job]", "Phải làm thứ 7")
}
