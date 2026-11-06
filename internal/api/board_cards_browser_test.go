//go:build browser

package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func newCardsBoard(t *testing.T) (*store.Store, *service.Service, *httptest.Server) {
	t.Helper()
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	t.Cleanup(ts.Close)
	return st, svc, ts
}

func cardTitles(page *rod.Page, lane string) []string {
	var titles []string
	for _, v := range page.MustEval(`lane => [...document.querySelectorAll("#col-" + lane + " .card-title")].map(a => a.textContent.trim())`, lane).Arr() {
		titles = append(titles, v.Str())
	}
	return titles
}

// A long lane shows its best-rated cards and a button for the rest; a job
// behind that button still opens from elsewhere. Lanes with nothing in them
// give their room to the lane that has cards.
func TestBoardLongLane(t *testing.T) {
	st, _, ts := newCardsBoard(t)
	ctx := context.Background()
	now := time.Now()
	for i := 1; i <= 14; i++ {
		st.Add(ctx, jobs.Job{Title: fmt.Sprintf("Unrated %d", i), Status: "saved"}, now.Add(time.Duration(i)*time.Minute))
	}
	st.Add(ctx, jobs.Job{Title: "Weak", Status: "saved", Score: 1.5, HasScore: true, ScoreLetter: "D", ScoreStatus: "scored"}, now)
	st.Add(ctx, jobs.Job{Title: "Strong", Status: "saved", Score: 4.6, HasScore: true, ScoreLetter: "A", ScoreStatus: "scored"}, now)
	hidden, _ := st.Add(ctx, jobs.Job{Title: "Oldest unrated", Status: "saved"}, now.Add(-time.Hour))

	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(1280, 900, 1, false)
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")

	titles := cardTitles(page, "Saved")
	if len(titles) != 10 || titles[0] != "Strong" || titles[1] != "Weak" {
		t.Fatalf("Saved shows %q, want 10 cards with the rated ones first", titles)
	}
	// The order can still be switched, by keyboard too.
	page.MustElement("#col-Saved .column-sort").MustFocus()
	page.Keyboard.MustType(input.Enter)
	page.MustWait(`() => document.querySelector("#col-Saved .card-title").textContent.trim() === "Unrated 14"`)
	if got := page.MustEval(`() => document.activeElement.textContent`).Str(); !strings.Contains(got, "Newest first") {
		t.Fatalf("focus after sorting is on %q", got)
	}
	page.MustElement("#col-Saved .column-sort").MustEval(`() => this.click()`)

	more := page.MustElement("#col-Saved .lane-more")
	if got := more.MustText(); got != "Show 7 more" {
		t.Fatalf("button says %q", got)
	}
	// A job past the cut opens without opening the whole lane.
	page.MustEval(`id => showJobCard(String(id))`, hidden)
	if got := len(cardTitles(page, "Saved")); got != 11 {
		t.Fatalf("%d cards after showing a hidden job, want 11", got)
	}
	if got := page.MustElement("#col-Saved .lane-more").MustText(); got != "Show 6 more" {
		t.Fatalf("button says %q", got)
	}
	page.MustElement("#col-Saved .lane-more").MustFocus()
	page.Keyboard.MustType(input.Enter)
	page.MustWait(`() => document.querySelectorAll("#col-Saved .card").length === 17`)
	if got := page.MustEval(`() => document.activeElement.textContent`).Str(); got != "Show fewer" {
		t.Fatalf("focus after Show more is on %q", got)
	}

	// Only Saved has cards: it takes the room, and the empty lanes are strips.
	sizes := page.MustEval(`() => ({
		saved: document.getElementById("col-Saved").getBoundingClientRect().width,
		empty: document.getElementById("col-Offer").getBoundingClientRect().width,
		emptyHeight: document.getElementById("col-Offer").getBoundingClientRect().height,
		columns: getComputedStyle(document.getElementById("cards-Saved")).gridTemplateColumns.split(" ").length,
	})`)
	if sizes.Get("saved").Num() < 800 || sizes.Get("empty").Num() > 50 || sizes.Get("emptyHeight").Num() > 150 || sizes.Get("columns").Int() < 3 {
		t.Fatalf("lane sizes = %v", sizes)
	}

	// A lane header works from the keyboard, and keeps the focus.
	page.MustElement("#col-Offer .column-header").MustFocus()
	page.Keyboard.MustType(input.Enter)
	page.MustWait(`() => !document.getElementById("col-Offer").classList.contains("folded")`)
	if !page.MustEval(`() => document.activeElement.closest("#col-Offer") !== null`).Bool() {
		t.Fatal("focus lost after unfolding a lane")
	}

	// A phone: no sideways scrolling, and an empty lane is one short row.
	setViewportWidth(page, 390)
	page.MustEval(`() => renderBoard()`)
	if problem := page.MustEval(`() => {
		const doc = document.documentElement;
		if (doc.scrollWidth > doc.clientWidth) return "page scrolls sideways: " + doc.scrollWidth;
		const h = document.getElementById("col-Interview").getBoundingClientRect().height;
		return h > 50 ? "empty lane is " + h + "px tall" : "";
	}`).Str(); problem != "" {
		t.Fatal(problem)
	}
}

// A closed card is short: title, company, pay and one warning. Everything else
// is inside the open card, and what stays has a name you can read or hear.
func TestBoardCompactCard(t *testing.T) {
	_, svc, ts := newCardsBoard(t)
	ctx := context.Background()
	warned, _ := svc.AddJob(ctx, service.NewJob{
		Title: "Six days", Company: "Duck Co", Salary: "Thương lượng", URL: "https://www.vietnamworks.com/six-days-1-jv",
		Source: "vietnamworks", JD: "Thời gian làm việc: T2-T7, 8h-17h.",
	})
	svc.AddJob(ctx, service.NewJob{Title: "Unrated", Company: "Vịt Vàng", Salary: "16000000 - 22000000 VND/month"})

	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(1280, 900, 1, false)
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	card := func(title string) string {
		return fmt.Sprintf(`[...document.querySelectorAll(".card")].find(c => c.querySelector(".card-title").textContent.trim() === %q)`, title)
	}

	if n := page.MustEval(`() => ` + card("Six days") + `.querySelectorAll(".badge.dealbreaker, .expiry-badge").length`).Int(); n != 1 {
		t.Fatalf("%d warning pills on a closed card, want 1", n)
	}
	pill := page.MustEval(`() => { const p = ` + card("Six days") + `.querySelector(".badge.dealbreaker"); return [p.textContent, p.title]; }`).Arr()
	if !strings.Contains(pill[0].Str(), "+1") || !strings.Contains(pill[1].Str(), "Lương thỏa thuận") || !strings.Contains(pill[1].Str(), "Làm T7") {
		t.Fatalf("pill = %v, want the first warning, +1, and all of them in its tooltip", pill)
	}
	if text := page.MustEval(`() => ` + card("Six days") + `.innerText`).Str(); strings.Contains(strings.ToLower(text), "vietnamworks") {
		t.Fatalf("closed card has more than it should: %q", text)
	}
	if page.MustEval(`() => !!` + card("Unrated") + `.querySelector(".score-badge")`).Bool() {
		t.Fatal(`an unrated card shows a score badge (it was a grey "—")`)
	}
	if got := page.MustEval(`() => ` + card("Unrated") + `.querySelector(".salary-chip").textContent`).Str(); !strings.Contains(got, "16–22 triệu/tháng") {
		t.Fatalf("pay = %q", got)
	}
	if h := page.MustEval(`() => ` + card("Six days") + `.getBoundingClientRect().height`).Num(); h > 160 {
		t.Fatalf("closed card is %vpx tall", h)
	}
	// No unnamed icons: each control on a closed card has words and a tooltip.
	if bad := page.MustEval(`() => [...document.querySelectorAll(".card a.card-link, .card button, .card select")]
		.filter(el => !el.title || !/\p{L}/u.test(el.textContent)).map(el => el.outerHTML.slice(0, 100))`).Arr(); len(bad) > 0 {
		t.Fatalf("control without a name or tooltip: %v", bad[0])
	}
	if got := page.MustEval(`() => ` + card("Six days") + `.querySelector("a.card-link").href`).Str(); !strings.Contains(got, "vietnamworks.com/six-days") {
		t.Fatalf("Open link = %q", got)
	}

	// Clicking the card itself opens it; the warnings and source are inside.
	page.MustEval(`() => ` + card("Six days") + `.querySelector(".card-meta").click()`)
	facts := page.MustElement(fmt.Sprintf("#detail-%d .card-facts", warned.ID))
	text := facts.MustText()
	for _, want := range []string{"Lương thỏa thuận", "Làm T7", "VietnamWorks"} {
		if !strings.Contains(text, want) {
			t.Errorf("open card is missing %q: %q", want, text)
		}
	}
	if n := page.MustEval(`() => ` + card("Six days") + `.querySelectorAll(".card-meta .badge").length`).Int(); n != 0 {
		t.Error("open card repeats the warning pill")
	}

	// No more source checkboxes; the search box finds a source.
	if page.MustHas(`input[name="platform-filter"]`) {
		t.Fatal("source checkboxes are back")
	}
	page.MustElement("#jobSearch").MustInput("vietnamworks")
	if got := cardTitles(page, "Saved"); len(got) != 1 || got[0] != "Six days" {
		t.Fatalf("searching a source found %q", got)
	}
}

// An open card has one main action, for its stage. The rest waits under More,
// which stays open across refreshes. Moving a job down the stages works from
// that button alone.
func TestBoardOpenCardStages(t *testing.T) {
	st, svc, ts := newCardsBoard(t)
	ctx := context.Background()
	if _, err := svc.SetRatingConfig(ctx, rating.Config{Models: []string{"claude-opus-5-5"}, DefaultModel: "claude-opus-5-5"}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(ctx, jobs.Job{Title: "Go Engineer", URL: "https://example.com/go", Status: "saved", ExpiresAt: "expire:2099-01-01"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitRating(ctx, service.Verdict{JobID: id, Model: "claude-opus-5-5", Score: 4.2, Reasoning: "One • Two • Three • Four"}); err != nil {
		t.Fatal(err)
	}
	noLink, _ := svc.AddJob(ctx, service.NewJob{Title: "Pasted job"})

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustEval(`id => toggleCardDetail(String(id))`, id)
	detail := fmt.Sprintf("#detail-%d", id)
	page.MustElement(detail)

	primary := func() string {
		page.MustWait(`sel => document.querySelectorAll(sel + " .card-primary").length === 1`, detail)
		return page.MustElement(detail + " .card-primary").MustText()
	}
	if got := primary(); !strings.Contains(got, "Open the job") {
		t.Fatalf("Saved: main action = %q", got)
	}
	if got := page.MustElement(detail + " a.card-primary").MustProperty("href").Str(); got != "https://example.com/go" {
		t.Fatalf("main action links to %q", got)
	}
	// Three reasons show; everything else waits under More.
	if n := page.MustEval(`sel => document.querySelector(sel + " > .detail-section").querySelectorAll("li").length`, detail).Int(); n != 3 {
		t.Fatalf("%d reasons above More, want 3", n)
	}
	moreOpen := func() bool {
		return page.MustEval(`sel => document.querySelector(sel + " .card-more").open`, detail).Bool()
	}
	if moreOpen() {
		t.Fatal("More is open before anyone asked for it")
	}
	if !page.MustEval(`sel => !!document.querySelector(sel + " .card-more textarea")`, detail).Bool() {
		t.Fatal("notes aren't under More")
	}

	page.MustElement(detail + " .card-more > summary").MustEval(`() => this.click()`)
	page.MustWait(`sel => document.querySelector(sel + " .card-more").open`, detail)
	text := page.MustElement(fmt.Sprintf(`.card[data-id="%d"]`, id)).MustText()
	for _, not := range []string{"claude-opus-5-5", "CLAUDE-OPUS", "Exempt", "Auto-clear", "Auto-delete"} {
		if strings.Contains(text, not) {
			t.Errorf("open card says %q:\n%s", not, text)
		}
	}
	if !strings.Contains(text, "Four") {
		t.Errorf("the fourth reason is lost:\n%s", text)
	}

	// Keep is a switch, it reaches the server, and More and the keyboard focus
	// both survive the refresh it causes.
	keep := detail + ` .keep-toggle`
	page.MustElement(keep).MustFocus()
	page.Keyboard.MustType(input.Enter)
	waitUntil(t, func() bool { jj, _ := svc.GetJob(ctx, id); return jj.Keep }, "Keep didn't reach the server")
	page.MustWait(`sel => document.querySelector(sel).getAttribute("aria-checked") === "true"`, keep)
	if !page.MustEval(`sel => document.activeElement === document.querySelector(sel)`, keep).Bool() {
		t.Error("focus left the Keep switch after it refreshed")
	}
	if !moreOpen() {
		t.Error("a refresh closed More")
	}

	// Each stage's button moves it on.
	for _, step := range []struct{ from, label, to string }{
		{"applied", "Got an interview", "interview"},
		{"interview", "Got an offer", "offer"},
		{"offer", "Close this job", "closed"},
		{"closed", "Back to Saved", "saved"},
	} {
		if _, err := svc.SetStatus(ctx, id, step.from); err != nil {
			t.Fatal(err)
		}
		page.MustEval(`async () => { await refreshAppState(); }`)
		if got := primary(); got != step.label {
			t.Fatalf("%s: main action = %q, want %q", step.from, got, step.label)
		}
		page.MustElement(detail + " .card-primary").MustEval(`() => this.click()`)
		waitUntil(t, func() bool { jj, _ := svc.GetJob(ctx, id); return jj.Status == step.to }, step.label+" didn't move the job")
	}

	// A job with no link can't be opened: "I applied" takes the place.
	page.MustEval(`id => toggleCardDetail(String(id))`, noLink.ID)
	noLinkPrimary := fmt.Sprintf("#detail-%d .card-primary", noLink.ID)
	if got := page.MustElement(noLinkPrimary).MustText(); got != "I applied" {
		t.Fatalf("no link: main action = %q", got)
	}
	if page.MustHas(fmt.Sprintf(`.card[data-id="%d"] a.card-link`, noLink.ID)) {
		t.Fatal("a job without a link has an Open link")
	}

	// A phone: the main action is big enough to tap and nothing overflows.
	setViewportWidth(page, 390)
	page.MustEval(`() => renderBoard()`)
	waitVisible(page, noLinkPrimary)
	box := page.MustEval(`sel => { const r = document.querySelector(sel).getBoundingClientRect(); return [r.width, r.height, document.documentElement.scrollWidth, document.documentElement.clientWidth]; }`, noLinkPrimary).Arr()
	if box[0].Num() < 250 || box[1].Num() < 36 || box[2].Int() > box[3].Int() {
		t.Fatalf("on a phone the main action is %v wide, %v tall; page %v in %v", box[0], box[1], box[2], box[3])
	}
}

// Day one has nothing to report: no interview line, no always-moving log. The
// top is one short strip, and 🔔 opens recent activity.
func TestBoardTopStrip(t *testing.T) {
	_, svc, ts := newCardsBoard(t)
	ctx := context.Background()
	svc.AddJob(ctx, service.NewJob{Title: "Fresh <b>job</b>", Source: "careerlink"})

	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(390, 844, 1, true)
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustWait(`() => document.getElementById("todaySummary").textContent !== ""`)

	if !page.MustEval(`() => document.getElementById("moraleWidget").hidden`).Bool() {
		t.Fatal("the interview line shows before there is any answer")
	}
	if strings.Contains(page.MustEval(`() => document.body.innerText`).Str(), "0 interviews") {
		t.Fatal(`the page says "0 interviews" on day one`)
	}
	if h := page.MustElement("#todayWidget").MustEval(`() => this.getBoundingClientRect().height`).Num(); h > 60 {
		t.Fatalf("Today strip is %vpx tall", h)
	}
	if page.MustHas(".system-log") {
		t.Fatal("the bottom log bar is back")
	}

	// On a phone, without Get started, the board starts near the top.
	if y := page.MustEval(`() => document.getElementById("kanbanBoard").getBoundingClientRect().top - (document.getElementById("setupCard").hidden ? 0 : document.getElementById("setupCard").getBoundingClientRect().height)`).Num(); y > 300 {
		t.Fatalf("the board starts %vpx down", y)
	}

	panel := page.MustElement("#activityPanel")
	if panel.MustVisible() {
		t.Fatal("recent activity is open on arrival")
	}
	page.MustElement("#activityBtn").MustFocus()
	page.Keyboard.MustType(input.Enter)
	page.MustWait(`() => !document.getElementById("activityPanel").hidden`)
	if got := panel.MustText(); !strings.Contains(got, "Recent activity") || !strings.Contains(got, "Fresh <b>job</b>") {
		t.Fatalf("activity = %q", got)
	}
	if page.MustHas("#activityPanel b") {
		t.Fatal("a job title became markup in the activity list")
	}
	if !page.MustEval(`() => { const r = document.getElementById("activityPanel").getBoundingClientRect(); return r.left >= 0 && r.right <= document.documentElement.clientWidth; }`).Bool() {
		t.Fatal("recent activity doesn't fit a phone")
	}
	page.Keyboard.MustType(input.Escape)
	page.MustWait(`() => document.getElementById("activityPanel").hidden`)
	if !page.MustEval(`() => document.activeElement.id === "activityBtn"`).Bool() {
		t.Fatal("Esc didn't return focus to the 🔔 button")
	}
	if got := page.MustElement("#activityBtn").MustAttribute("aria-expanded"); got == nil || *got != "false" {
		t.Fatalf("aria-expanded = %v", got)
	}
}
