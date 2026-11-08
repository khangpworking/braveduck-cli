//go:build browser

package api

import (
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/input"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// The list sorts by fit, pay, distance or deadline, per stage, remembered.
func TestAppSortsTheList(t *testing.T) {
	_, _, url := appBoard(t, []jobs.Job{
		{Title: "Best fit, low pay", Status: "saved", Score: 4.8, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored", Salary: "10000000 - 12000000 VND/month"},
		{Title: "Weak fit, high pay", Status: "saved", Score: 3.1, HasScore: true, ScoreLetter: "D", ScoreStatus: "scored", Salary: "25 - 30 triệu"},
		{Title: "Pay not given", Status: "saved", Salary: "Thỏa thuận"},
	})
	page := appPage(t, url, 1280)
	titles := func() string {
		return page.MustEval(`() => [...document.querySelectorAll("[data-job]")].map(e => e.innerText.split("\n")[1]).join("|")`).String()
	}
	if got := titles(); !strings.HasPrefix(got, "Best fit, low pay|Weak fit, high pay") {
		t.Fatalf("by fit: %s", got)
	}
	page.MustElement(`button[aria-label^="Sort:"]`).MustEval(`() => this.dispatchEvent(new PointerEvent("pointerdown", {bubbles: true, button: 0, pointerType: "mouse"}))`)
	clickText(page, "[role=menuitemradio]", "Highest pay")
	page.MustWait(`() => document.querySelector("[data-job]").innerText.includes("Weak fit, high pay")`)
	if got := titles(); got != "Weak fit, high pay|Best fit, low pay|Pay not given" {
		t.Errorf("by pay: %s", got)
	}
	page.MustReload().MustWaitLoad()
	page.MustElementR(`button[aria-label^="Sort:"]`, "Highest pay")
}

// What's due shows in the row itself: a posting closing soon, an application
// a week without an answer.
func TestAppShowsWhatsDueInTheRow(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	page.MustElementR("[data-job]", "Accountant[\\s\\S]*2 days left to apply")
	clickText(page, "nav button", "Applied")
	page.MustElementR("[data-job]", "Tour accountant[\\s\\S]*No answer for 8 days")
	if page.MustHasR("[data-job]", "Warehouse accountant[\\s\\S]*No answer") {
		t.Error("an application 3 days old is flagged")
	}
}

// A job's page is in named parts, and an application shows its steps so far
// with the dates braveduck knows.
func TestAppJobPageHasPartsAndSteps(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	clickText(page, "nav button", "Applied")
	clickText(page, "[data-job]", "Tour accountant")
	detail := page.MustElement("section[aria-label='Tour accountant']")
	for _, part := range []string{"How it fits you", "Your part", "So far", "About the posting"} {
		detail.MustElement(`section[aria-label="` + part + `"]`)
	}
	steps := detail.MustElement(`section[aria-label="So far"]`).MustText()
	if !strings.Contains(steps, "Saved") || !strings.Contains(steps, "Applied") {
		t.Errorf("steps = %q", steps)
	}
	applied := time.Now().AddDate(0, 0, -8)
	if day := applied.Format("2"); !strings.Contains(steps, day) {
		t.Errorf("the applied day (%s) isn't shown: %q", day, steps)
	}
}

// "Go through them": one job a screen, keys answer, the next comes by
// itself, and the end says what was done.
func TestAppQuickReview(t *testing.T) {
	st, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	clickText(page, "button", "Go through them one by one")
	page.MustElementR("[role=dialog]", "1 of 3")
	first := page.MustElement("[data-review-job]").MustAttribute("data-review-job")
	page.Keyboard.MustType(input.ArrowRight) // later
	page.MustWait(`id => document.querySelector("[data-review-job]")?.dataset.reviewJob !== id`, *first)
	skipped := page.MustElement("[data-review-job] h3").MustText()
	page.Keyboard.MustType('x') // not a fit
	page.MustElementR("[role=dialog]", "3 of 3")
	page.Keyboard.MustType(input.ArrowRight)
	page.MustElementR("[data-review-done]", "skipped 1, kept 2 for later")
	waitUntil(t, func() bool { return jobStatus(t, st, skipped) == "closed" }, "X didn't close "+skipped)
}

// On a phone the profile form comes in parts, one a screen.
func TestAppProfileInPartsOnAPhone(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 390)
	page.MustElement(`button[aria-label="Settings"]`).MustEval(`() => this.click()`)
	clickText(page, "button", "Your profile")
	page.MustElementR("[data-chunked]", "Part 1 of 5")
	if page.MustHasR("[data-chunked]", "2. Pay") {
		t.Error("part 2 shows with part 1")
	}
	clickText(page, "[data-chunked] button", "^Next")
	page.MustElementR("[data-chunked]", "Part 2 of 5[\\s\\S]*2. Pay")
}

// The list leaves the posting's long text out; the job's page fetches it.
func TestAppJobPageFetchesItsText(t *testing.T) {
	_, _, url := appBoard(t, []jobs.Job{{Title: "Accountant", Status: "saved", AISummary: "- Closes the books every month\n- Works with SAP"}})
	page := appPage(t, url, 1280)
	if strings.Contains(page.MustEval(`() => JSON.stringify(window.performance.getEntriesByType("resource").map(r => r.name))`).String(), "/api/jobs\"") {
		t.Error("the board fetched the full job list")
	}
	clickText(page, "button", "About the job")
	page.MustElementR("section[aria-label='Accountant'] li", "Closes the books every month")
}

// The business register's answer sits under the company's name, linking to
// the company's page there; with no match, to the register's search.
func TestAppJobPageShowsTheCompanyRegister(t *testing.T) {
	_, _, url := appBoard(t, []jobs.Job{
		{Title: "Accountant", Company: "Công ty TNHH Thiên Ấn", Status: "saved", CompanyActiveDate: "2009-10-20", CompanyTaxCode: "0309542960",
			CompanyInfoURL: "https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-x.html", CompanyRegStatus: "NNT đã được cấp MST"},
		{Title: "Developer", Company: "FPT Software", Status: "saved", CompanyActiveDate: jobs.CompanyNotFound,
			CompanyInfoURL: "https://dulieuphapluat.vn/cong-cu/tra-cuu-ma-so-thue.html?keyword=FPT+SOFTWARE"},
	})
	page := appPage(t, url, 1280)
	clickText(page, "[data-job]", "Accountant")
	p := page.MustElementR("section[aria-label='Accountant'] p", "In business since")
	if got := p.MustText(); !strings.Contains(got, "20/10/2009") || !strings.Contains(got, "Tax code 0309542960") {
		t.Errorf("register line: %q", got)
	}
	a := p.MustElement("a")
	if href, target := a.MustAttribute("href"), a.MustAttribute("target"); *href != "https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-x.html" || *target != "_blank" {
		t.Errorf("company link: %s (target %s)", *href, *target)
	}
	clickText(page, "[data-job]", "Developer")
	a = page.MustElementR("section[aria-label='Developer'] p a", "Look it up on dulieuphapluat.vn")
	if href := a.MustAttribute("href"); !strings.Contains(*href, "tra-cuu-ma-so-thue.html?keyword=") {
		t.Errorf("search link: %s", *href)
	}
}

// Job sites show their own icon, bundled with the board: in the list (named
// for screen readers, as nothing else there says the site), and next to the
// site's name on the job's page.
func TestAppSiteIcons(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	icon := page.MustElement("[data-job] img[data-site-icon=topcv]")
	if alt := icon.MustAttribute("alt"); *alt != "TopCV" {
		t.Errorf("list icon alt = %q, want the site's name", *alt)
	}
	clickText(page, "[data-job]", "Accountant")
	page.MustElementR("section[aria-label='Accountant'] p", "Found on TopCV").MustElement("img[data-site-icon=topcv]")
	broken := page.MustEval(`() => [...document.querySelectorAll('img[data-site-icon]')].filter(i => !i.complete || i.naturalWidth === 0 || !i.src.startsWith(location.origin + '/assets/') && !i.src.startsWith('data:image/png')).map(i => i.src).join(' ')`).Str()
	if broken != "" {
		t.Errorf("icons that didn't load from the board itself: %s", broken)
	}
	if n := page.MustEval(`() => document.querySelectorAll('[data-job] img[data-site-icon]').length`).Int(); n != 1 {
		t.Errorf("%d list icons, want 1 (the one job found on a known site)", n)
	}
}
