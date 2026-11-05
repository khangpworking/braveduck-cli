//go:build browser

package browser

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

// With BRAVEDUCK_BROWSER_SERVICE set, browsers run in the browser service
// (here in-process; on a shared server its own container) using its key;
// without the key, or for a window, nothing starts.
func TestBrowserService(t *testing.T) {
	bin := os.Getenv("CLOAKBROWSER_BINARY_PATH")
	if bin == "" {
		found, ok := launcher.LookPath()
		if !ok {
			t.Skip("no Chromium found")
		}
		bin = found
	}
	const key = "0123456789abcdef0123456789abcdef-browser"
	svc := httptest.NewServer(Service(bin, key))
	defer svc.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	os.WriteFile(keyFile, []byte(key+"\n"), 0o600)
	t.Setenv(BrowserServiceEnv, "ws"+strings.TrimPrefix(svc.URL, "http"))
	t.Setenv(BrowserKeyFileEnv, keyFile)

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

	t.Setenv(BrowserKeyFileEnv, "")
	_, err = RenderPDF(ctx, "<p>x</p>")
	if err == nil {
		t.Fatal("a browser started without the key")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the error shows the key: %v", err)
	}
}
