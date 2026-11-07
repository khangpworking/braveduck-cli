//go:build browser

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/input"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// Job text comes from job sites and pasted posts, so it's hostile: nothing in
// it may run as script, in the list, the job, its notes or its edit form.
func TestAppHostileJobTextStaysText(t *testing.T) {
	evil := `<img src=x onerror="window.__pwned=1">`
	_, _, url := appBoard(t, []jobs.Job{{
		Title: "Accountant " + evil, Company: evil, Salary: evil, Location: evil, Notes: evil,
		ScoreStatus: "scored", Score: 4.6, HasScore: true, ScoreLetter: "A", ScoreReasoning: evil + " • fine", Status: "saved",
		URL: `javascript:window.__pwned=1`,
	}})
	page := appPage(t, url, 1280)
	page.MustElementR("section h1", "Accountant <img")
	clickText(page, "button", "My notes")
	openMoreMenu(page)
	clickText(page, "[role=menuitem]", "Edit details")
	page.MustElement("[role=dialog] input")
	if page.MustEval(`() => window.__pwned === 1 || !!document.querySelector("img[src=x]")`).Bool() {
		t.Fatal("job text ran as HTML")
	}
	// A javascript: link isn't a link to follow.
	if href := page.MustEval(`() => [...document.querySelectorAll("a")].map(a => a.getAttribute("href")).join(" ")`).String(); strings.Contains(href, "javascript:") {
		t.Errorf("a javascript: URL became a link: %s", href)
	}
}

// Every control has a name a screen reader can say: its text or an aria-label.
func TestAppButtonsHaveNames(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	for _, width := range []int{1280, 390} {
		page := appPage(t, url, width)
		check := func(where string) {
			t.Helper()
			unnamed := page.MustEval(`() => [...document.querySelectorAll("button, a[href], [role=menuitem], [role=tab], input, textarea")]
				.filter(e => e.getClientRects().length)
				.filter(e => {
					const name = (e.getAttribute("aria-label") || e.labels?.[0]?.textContent || e.getAttribute("placeholder") || e.textContent || "").trim();
					return !/\p{L}/u.test(name);
				})
				.map(e => e.outerHTML.slice(0, 120))`).Arr()
			for _, u := range unnamed {
				t.Errorf("%d px, %s: no name: %s", width, where, u.String())
			}
		}
		check("the list")
		if width < 760 {
			clickText(page, "[data-job]", "Accountant")
			page.MustElement("section[aria-label='Accountant']")
		}
		check("a job")
		openMoreMenu(page)
		check("the job's menu")
	}
}

// "/" goes to the search box, "n" opens Add job, and neither fires while the
// user is typing.
func TestAppShortcuts(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	page.Keyboard.MustType(input.Slash)
	page.MustWait(`() => document.activeElement?.type === "search"`)
	page.Keyboard.MustType('n')
	if page.MustHas("[role=dialog]") {
		t.Fatal("typing n in the search box opened Add job")
	}
	page.MustEval(`() => document.activeElement.blur()`)
	page.Keyboard.MustType('n')
	page.MustElement(`textarea[aria-label="Link or text of the posting"]`)
	page.Keyboard.MustType(input.Escape)
	page.MustWait(`() => !document.querySelector("[role=dialog]")`)
}

// With answers in, the applications show the interview rate and the
// server's line for it (get_stats), not a sum of the browser's own.
func TestAppMoraleFromServer(t *testing.T) {
	list := appJobs()
	for i := range 3 {
		list = append(list, jobs.Job{Title: "Answered " + string(rune('A'+i)), Status: "interview"})
	}
	list = append(list, jobs.Job{Title: "Turned down", Status: "closed"})
	_, svc, url := appBoard(t, list)
	st, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page := appPage(t, url, 1280)
	clickText(page, "nav button", "Applied")
	line := page.MustElementR("p", "interviews from").MustText()
	if !strings.Contains(line, "interviews from") || !strings.Contains(page.MustElement("section").MustText(), st.Morale) {
		t.Errorf("morale line = %q, want the server's %q", line, st.Morale)
	}
}

// The board lives at /, and the address it was tried out at leads there.
func TestAppOldAddressRedirects(t *testing.T) {
	_, _, url := appBoard(t, nil)
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
	resp, err := c.Get(strings.TrimSuffix(url, "/") + "/app/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != "/" {
		t.Errorf("/app/ = %d → %q, want 301 → /", resp.StatusCode, resp.Header.Get("Location"))
	}
}
