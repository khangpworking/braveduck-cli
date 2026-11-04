//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The "⛔ Làm T7" (works Saturdays) pill reads whole words and negations: a
// T2-T7 schedule shows it; "nghỉ thứ 7" and a code like HT7 don't.
func TestBoardSaturdayDealbreaker(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	for title, jd := range map[string]string{
		"Six days":    "Thời gian làm việc: T2-T7, 8h-17h. Lương 15 triệu.",
		"Weekend off": "Làm việc thứ 2 - thứ 6, nghỉ thứ 7 và chủ nhật. Lương 15 triệu.",
		"Has a code":  "Mã tin HT7-2026, làm việc thứ 2 đến thứ 6. Lương 15 triệu.",
	} {
		svc.AddJob(ctx, service.NewJob{Title: title, JD: jd, Salary: "15 triệu"})
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	pills := page.MustEval(`() => Object.fromEntries([...document.querySelectorAll(".card")].map(c =>
		[c.querySelector(".card-title").textContent.trim(), c.textContent.includes("Làm T7")]))`).Map()
	for title, want := range map[string]bool{"Six days": true, "Weekend off": false, "Has a code": false} {
		if got := pills[title].Bool(); got != want {
			t.Errorf("%s: Làm T7 pill = %v, want %v (%v)", title, got, want, strings.TrimSpace(title))
		}
	}
}
