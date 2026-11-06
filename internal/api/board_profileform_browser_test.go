//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The form asks only what matters for ratings. What it doesn't ask (here a
// target pay written as text) survives a save, and location is set in its
// own window, not typed twice.
func TestBoardProfileFormIsShort(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	svc := service.New(st)
	svc.SetDataDir(dir)
	ctx := context.Background()
	p, _ := svc.Profile(ctx)
	svc.SaveProfile(ctx, strings.Replace(p.Text, "- Target:", "- Target: 40 triệu net", 1))
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	clickID(page, "profileBtn")
	waitVisible(page, "#profileFormBody .pf-tag-input")
	labels := page.MustEval(`() => [...document.querySelectorAll("#profileFormBody > .pf-section .pf-label")].map(l => l.textContent).join("|")`).Str()
	for _, gone := range []string{"What you're hoping for", "Benefits", "Where you live", "Longest commute", "When can you start", "visa"} {
		if strings.Contains(labels, gone) {
			t.Errorf("the form still asks %q (%s)", gone, labels)
		}
	}
	if sugg := page.MustEval(`() => [...document.querySelectorAll("#profileFormBody .pf-suggestion")].map(b => b.textContent).join("|")`).Str(); strings.Contains(sugg, "Night shifts") || !strings.Contains(sugg, "Working on Saturdays") {
		t.Errorf("deal-breaker suggestions = %q", sugg)
	}

	in := page.MustElement(`#profileFormBody input[aria-label="Job titles"]`)
	in.MustInput("Kế toán")
	page.MustEval(`() => document.querySelector('#profileFormBody input[aria-label="Job titles"]').dispatchEvent(new Event("blur"))`)
	clickID(page, "saveProfileBtn")
	page.MustElementR("#profileStatus", "Filled in")
	b, _ := os.ReadFile(filepath.Join(dir, "profile.md"))
	if !strings.Contains(string(b), "- Target roles / job titles: Kế toán") || !strings.Contains(string(b), "- Target: 40 triệu net") {
		t.Fatalf("profile after saving the form:\n%s", b)
	}

	page.MustElementR("#profileFormBody button.btn-link", "set your location").MustEval(`() => this.click()`)
	page.MustWait(`() => document.getElementById("locationModalOverlay").classList.contains("open") && !document.getElementById("profileModalOverlay").classList.contains("open")`)
}
