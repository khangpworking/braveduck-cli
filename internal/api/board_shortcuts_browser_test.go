//go:build browser

package api

import (
	"net/http/httptest"
	"testing"

	"github.com/go-rod/rod/lib/input"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// "n" opens Add Job with the paste box focused, Esc closes it (and an open
// window) but not while writing in a text area; typing in a field doesn't
// trigger shortcuts.
func TestBoardShortcuts(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
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
	page.MustElement("#kanbanBoard")
	drawerOpen := func() bool {
		return page.MustEval(`() => document.getElementById("addJobDrawer").classList.contains("open")`).Bool()
	}

	page.Keyboard.MustType('n')
	page.MustWait(`() => document.activeElement.id === "pasteJDTextarea"`)
	if !drawerOpen() {
		t.Fatal(`"n" didn't open Add Job`)
	}
	page.Keyboard.MustType('n') // typing in the paste box is text, not a shortcut
	if v := page.MustElement("#pasteJDTextarea").MustProperty("value").Str(); v != "n" {
		t.Fatalf("paste box = %q, want the typed n", v)
	}
	page.Keyboard.MustType(input.Escape) // still writing: nothing closes
	if !drawerOpen() {
		t.Fatal("Esc in the paste box closed Add Job (and its text)")
	}
	page.MustEval(`() => document.activeElement.blur()`)
	page.Keyboard.MustType(input.Escape)
	if drawerOpen() {
		t.Fatal("Esc didn't close Add Job")
	}

	clickID(page, "profileBtn")
	page.MustElement("#profileModalOverlay.open")
	page.MustEval(`() => document.activeElement.blur()`)
	page.Keyboard.MustType(input.Escape)
	page.MustWait(`() => !document.getElementById("profileModalOverlay").classList.contains("open")`)
}
