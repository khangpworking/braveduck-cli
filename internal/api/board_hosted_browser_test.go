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

	// Ask your AI shows how to connect an agent to this board over HTTPS.
	if _, err := svc.SaveProfile(context.Background(), "# About me\nGo developer.\n\n# What I'm looking for\nBackend roles.\n"); err != nil {
		t.Fatal(err)
	}
	j, err := svc.AddJob(context.Background(), service.NewJob{Title: "Go Engineer", Company: "Duck Co", JD: "Go."})
	if err != nil {
		t.Fatal(err)
	}
	page.MustEval(`id => askAI("cv", String(id))`, j.ID)
	page.MustWait(`() => !document.getElementById("askConnect").hidden`)
	page.MustElement("#agentKeyNew").MustEval(`() => this.click()`)
	page.MustWait(`() => !document.getElementById("askConnectCmd").hidden`)
	cmd := page.MustElement("#askConnectCmd").MustText()
	if !strings.Contains(cmd, "claude mcp add --transport http braveduck "+ts.URL+"/mcp") {
		t.Fatalf("connect command = %q", cmd)
	}
	_, key, ok := strings.Cut(cmd, "Authorization: Bearer ")
	key = strings.TrimSuffix(strings.TrimSpace(key), `"`)
	if !ok || !strings.HasPrefix(key, "alice<b>:") || !svc.CheckAgentKey(context.Background(), key) {
		t.Fatalf("the key shown (%q) isn't the board's agent key", key)
	}
	page.MustElement("#agentKeyRevoke").MustEval(`() => this.click()`)
	waitUntil(t, func() bool { return !svc.CheckAgentKey(context.Background(), key) }, "the key still works after Remove")
}
