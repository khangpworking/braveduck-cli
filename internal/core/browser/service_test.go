package browser

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
