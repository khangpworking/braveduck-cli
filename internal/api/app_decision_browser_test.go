//go:build browser

package api

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// Rating model: switch Jev ↔ Clef, paste each key (write-only: the field
// empties and the page never shows the key), pick the Clef model.
func TestAppRatingModelKeysAndSwitch(t *testing.T) {
	ctx := context.Background()
	const token, jevKey = "cf-browser-secret-913", "jev-browser-secret-271"
	page, svc := settingsApp(t, appJobs(), 1280, func(svc *service.Service) {
		dir := t.TempDir()
		key := make([]byte, 32)
		rand.Read(key)
		os.WriteFile(filepath.Join(dir, "session.key"), key, 0o600) // never the real keychain
		svc.SetDataDir(dir)
	})
	d := openWin(t, page, "^Rating$", "How jobs get rated")
	waitText(t, d, "Rating model")

	tap(page, "[role=dialog] button", `^Clef \(Cloudflare\)$`)
	waitUntil(t, func() bool { return must(svc.DecisionSettings(ctx)).Provider == service.ProviderClef }, "the switch to Clef wasn't saved")

	page.MustElement("[role=dialog] #clef-account").MustInput("BD23A6717746B9B4EBB6F4C3C84727C7")
	page.MustElement("[role=dialog] #clef-token").MustInput(token)
	tap(page, "[role=dialog] button", "^Save Clef settings$")
	waitUntil(t, func() bool {
		s := must(svc.DecisionSettings(ctx))
		return s.Clef.Saved && s.ClefAccountID == "bd23a6717746b9b4ebb6f4c3c84727c7"
	}, "the Clef token and account weren't saved")
	page.MustWait(`() => document.querySelector("[role=dialog] #clef-token").value === ""`)
	page.MustElementR("[role=dialog] p", "sent to Cloudflare's Clef")

	tap(page, "[role=dialog] button", `^Careful \(clef\)$`)
	waitUntil(t, func() bool { return must(svc.DecisionSettings(ctx)).ClefModel == "clef" }, "the Clef model wasn't saved")

	page.MustElement("[role=dialog] #jev-key").MustInput(jevKey)
	tap(page, "[role=dialog] button", "^Save Jev key$")
	waitUntil(t, func() bool { return must(svc.DecisionSettings(ctx)).Jev.Saved }, "the Jev key wasn't saved")
	tap(page, "[role=dialog] button", `^Jev \(TypeSafe\)$`)
	waitUntil(t, func() bool { return must(svc.DecisionSettings(ctx)).Provider == service.ProviderJev }, "the switch back to Jev wasn't saved")

	html := page.MustHTML()
	if strings.Contains(html, "browser-secret") {
		t.Fatal("a pasted key is still on the page")
	}
}
