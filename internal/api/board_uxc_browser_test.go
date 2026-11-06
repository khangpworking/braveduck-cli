//go:build browser

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The Add Job drawer asks one short question and offers one obvious action,
// and it keeps working on a phone.
func TestBoardAddJobDrawerIsOneLine(t *testing.T) {
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
	setViewportWidth(page, 390)
	clickID(page, "openDrawerBtn")
	waitVisible(page, "#pasteJDTextarea")
	page.MustWait(`() => document.getElementById("addJobDrawer").getBoundingClientRect().right <= document.documentElement.clientWidth + 1`)

	text := page.MustElement("#addJobDrawer").MustText()
	if !strings.Contains(text, "Paste a job link, a Facebook group link, or the job text") {
		t.Fatalf("drawer lacks its one-line hint: %q", text)
	}
	for _, gone := range []string{"Extract & Add", "✨", "TopCV, VietnamWorks"} {
		if strings.Contains(text, gone) {
			t.Errorf("drawer still shows %q", gone)
		}
	}
	if got := page.MustEval(`() => document.getElementById("extractAddBtn").textContent.trim()`).Str(); got != "Add" {
		t.Fatalf("button = %q, want Add", got)
	}
	if problem := page.MustEval(`() => {
		const w = document.documentElement.clientWidth;
		const d = document.getElementById("addJobDrawer").getBoundingClientRect();
		const body = document.querySelector("#addJobDrawer .drawer-body");
		if (!body) return "no drawer body";
		if (body.scrollWidth > body.clientWidth + 1) return "drawer overflows: " + body.scrollWidth + " > " + body.clientWidth;
		for (const el of [document.getElementById("pasteJDTextarea"), document.getElementById("extractAddBtn")]) {
			const r = el.getBoundingClientRect();
			if (r.left < 0 || r.right > w) return el.id + " outside viewport " + w + ": " + JSON.stringify(r) + " drawer " + JSON.stringify(d);
		}
		return "";
	}`).Str(); problem != "" {
		t.Fatal(problem)
	}

	jp := "Tuyển dụng Backend Engineer\nLương 30-40 triệu gross\nHybrid, Hà Nội\nYêu cầu: 3 năm kinh nghiệm Go"
	page.MustElement("#pasteJDTextarea").MustInput(jp)
	clickID(page, "extractAddBtn")
	page.MustElement("#extractResultBanner").MustWaitVisible()
	if banner := page.MustElement("#extractResultBanner").MustText(); !strings.Contains(banner, "Job added") {
		t.Fatalf("banner = %q", banner)
	}
	waitUntil(t, func() bool {
		list, _ := svc.ListJobs(context.Background(), service.ListFilter{})
		return len(list) == 1
	}, "pasted job was not added")
	list, _ := svc.ListJobs(context.Background(), service.ListFilter{})
	if list[0].Title != "Tuyển dụng Backend Engineer" || list[0].Company != "" || list[0].Salary != "30-40 triệu gross" {
		t.Fatalf("pasted job = title %q, company %q, salary %q", list[0].Title, list[0].Company, list[0].Salary)
	}
}

// Rating before the profile is filled in sends the user to the profile form
// and never shows a server file path.
func TestBoardRateWithEmptyProfileOpensProfileForm(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	svc := service.New(st)
	svc.SetDataDir(dir)
	ctx := context.Background()
	j, err := svc.AddJob(ctx, service.NewJob{Title: "Go Engineer", Company: "Duck Co"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustEval(`id => toggleCardDetail(id)`, fmt.Sprint(j.ID))
	page.MustWait(`id => !!document.querySelector('#detail-' + id + ' button')`, fmt.Sprint(j.ID))
	page.MustEval(`id => [...document.querySelectorAll('#detail-' + id + ' button')].find(b => b.textContent.trim() === 'Rate').click()`, fmt.Sprint(j.ID))

	page.MustWait(`() => document.getElementById("profileModalOverlay").classList.contains("open")`)
	page.MustWait(`() => document.getElementById("toastContainer").textContent.includes("Answer a few questions first")`)
	if body := page.MustElement("body").MustText(); strings.Contains(body, dir) {
		t.Fatalf("board showed a filesystem path: %q", body)
	}
	if page.MustEval(`() => document.getElementById("askModalOverlay").classList.contains("open")`).Bool() {
		t.Fatal("an empty profile opened the copy-a-prompt window")
	}
}

// The rating window still opens when the Jev status can't be read; the Jev
// notice and opt-out are hidden instead.
func TestBoardRatingOpensWhenJevStatusFails(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	h := Handler(st, svc, testToken)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/jev" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"database is locked"}`))
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer ts.Close()

	page := launchBrowser(t).MustPage(ts.URL).MustWaitLoad()
	page.MustElement(".column")
	openModal(t, page, "ratingBtn", "ratingModalOverlay")
	if !page.MustEval(`() => document.getElementById("jevBlock").hidden`).Bool() {
		t.Fatal("Jev block shown although its status failed to load")
	}
	if text := page.MustElement("#ratingModalOverlay").MustText(); !strings.Contains(text, "Each job gets a grade") {
		t.Fatalf("rating window = %q", text)
	}
}
