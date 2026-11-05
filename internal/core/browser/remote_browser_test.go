//go:build browser

package browser

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

// With BRAVEDUCK_BROWSER_SERVICE set, browsers run in go-rod's browser
// service (here in-process; on a shared server its own container), and
// windows are refused there.
func TestBrowserService(t *testing.T) {
	m := launcher.NewManager()
	if bin := os.Getenv("CLOAKBROWSER_BINARY_PATH"); bin != "" {
		// Stand in for the service's own Chromium, as its image provides.
		m.Defaults = func(_ http.ResponseWriter, _ *http.Request) *launcher.Launcher { return launcher.New().Bin(bin) }
		m.BeforeLaunch = func(*launcher.Launcher, http.ResponseWriter, *http.Request) {}
	}
	svc := httptest.NewServer(m)
	defer svc.Close()
	t.Setenv(BrowserServiceEnv, "ws"+strings.TrimPrefix(svc.URL, "http"))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pdf, err := RenderPDF(ctx, "<h1>Alice</h1><p>Go developer.</p>")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("not a PDF: %q", pdf[:min(len(pdf), 20)])
	}
	if _, _, err := launch(ctx, launchOpts{Headless: false}); err == nil || !strings.Contains(err.Error(), "can't open") {
		t.Fatalf("a window in the browser service: %v", err)
	}
}
