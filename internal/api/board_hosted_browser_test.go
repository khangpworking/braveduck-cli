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

// On a shared server the board names the logged-in user with a log-out link,
// and the logins panel explains why it can't open login windows instead of
// offering Connect (a saved login can still be removed).
func TestBoardOnSharedServer(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	svc.SetHosted(service.HostedInfo{User: "alice<b>", LogoutURL: "https://auth.example.com/logout"})
	st.SaveSession(context.Background(), store.PlatformSession{Platform: "topcv", Enc: []byte{1}, CreatedAt: "2026-11-01T00:00:00Z",
		FPSeed: "1", Status: store.SessionConnected})
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustWait(`() => !document.getElementById("hostedUser").hidden`)
	user := page.MustElement("#hostedUser")
	if got := user.MustText(); !strings.HasPrefix(got, "alice<b>") || !strings.Contains(got, "Log out") {
		t.Fatalf("user line = %q (the name must show as text)", got)
	}
	if href := user.MustElement("a").MustProperty("href").String(); href != "https://auth.example.com/logout" {
		t.Fatalf("log-out link = %q", href)
	}

	openModal(t, page, "platformsBtn", "platformsModalOverlay")
	page.MustElement("#platformsList strong")
	text := page.MustElement("#platformsList").MustText()
	if !strings.Contains(text, "off on this shared server") {
		t.Fatalf("no shared-server note:\n%s", text)
	}
	if strings.Contains(text, "Connect") || strings.Contains(text, "Log in again") {
		t.Fatalf("login buttons offered on a shared server:\n%s", text)
	}
	if !strings.Contains(text, "Disconnect") {
		t.Fatalf("a saved login can't be removed:\n%s", text)
	}
}
