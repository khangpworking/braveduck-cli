//go:build browser

package api

import (
	"net/http/httptest"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The board speaks Vietnamese by default; ⚙ Settings → 🌐 English switches
// it, the choice sticks across reloads, and text with no translation yet
// shows in English rather than blank.
func TestBoardLanguage(t *testing.T) {
	t.Setenv("BRAVEDUCK_LANG", "") // the shipped default, not the tests' English
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustWait(`() => document.getElementById("openDrawerBtn").textContent === "+ Thêm việc"`)
	if lang := page.MustEval(`() => document.documentElement.lang`).Str(); lang != "vi" {
		t.Fatalf("html lang = %q", lang)
	}
	if got := page.MustEval(`() => t("No translation for this yet")`).Str(); got != "No translation for this yet" {
		t.Fatalf("missing translation = %q", got)
	}
	if got := page.MustEval(`() => t("{n} new jobs", {n: 3})`).Str(); got != "3 new jobs" {
		t.Fatalf("placeholder = %q", got)
	}
	if got := page.MustElement("#langBtn").MustText(); got != "🌐 English" {
		t.Fatalf("switch = %q", got)
	}

	wait := page.WaitNavigation("load")
	page.MustElement("#langBtn").MustEval(`() => this.click()`)
	wait()
	page.MustWait(`() => document.getElementById("openDrawerBtn").textContent === "+ Add Job"`)
	page.MustReload().MustWaitLoad()
	page.MustWait(`() => document.getElementById("openDrawerBtn").textContent === "+ Add Job" && document.documentElement.lang === "en"`)
	if got := page.MustElement("#langBtn").MustText(); got != "🌐 Tiếng Việt" {
		t.Fatalf("switch back = %q", got)
	}
}
