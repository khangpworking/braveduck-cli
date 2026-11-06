package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The browser service answers nothing without its key — not even its
// launch settings — so a page can't use it to start browsers.
func TestServiceNeedsTheKey(t *testing.T) {
	h := Service("/nonexistent/chrome", "0123456789abcdef0123456789abcdef")
	for _, target := range []string{"/", "/?key=", "/?key=wrong", "/?key=0123456789abcdef0123456789abcdeX"} {
		r := httptest.NewRequest("GET", target, nil)
		r.Header.Set("Upgrade", "websocket")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", target, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?key=0123456789abcdef0123456789abcdef", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("with the key: %d", w.Code)
	}
}

// Connection errors can quote the HTTP bootstrap URL, even when the service
// was configured with a WebSocket URL. Neither spelling may expose its key.
func TestServiceConnectionErrorsHideKey(t *testing.T) {
	const key = "dummy-browser-service-key /+"
	const escapedKey = "%64ummy-browser-service-key%20%2f%2b"
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer svc.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"http", "ws"} {
		for _, source := range []struct{ name, parameter, value string }{
			{"file", "", ""},
			{"url", "key", url.QueryEscape(key)},
			{"url-escaped", "key", escapedKey},
			{"url-escaped-name", "%6bey", escapedKey},
		} {
			t.Run(scheme+"/"+source.name, func(t *testing.T) {
				addr := scheme + strings.TrimPrefix(svc.URL, "http")
				t.Setenv(BrowserKeyFileEnv, "")
				if source.name == "file" {
					t.Setenv(BrowserKeyFileEnv, keyFile)
				} else {
					addr += "?" + source.parameter + "=" + source.value
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _, err := launchRemote(ctx, addr, launchOpts{Headless: true})
				if err == nil {
					t.Fatal("expected a connection error")
				}
				for _, spelling := range []string{key, url.QueryEscape(key), source.value} {
					if spelling != "" && strings.Contains(err.Error(), spelling) {
						t.Fatal("connection error exposed the browser-service key")
					}
				}
				if !strings.Contains(err.Error(), strings.TrimPrefix(svc.URL, "http://")) {
					t.Fatalf("connection error lost the service address: %v", err)
				}
			})
		}
	}
}
