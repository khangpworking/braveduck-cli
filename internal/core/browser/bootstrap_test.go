package browser

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

func TestRemoteBootstrapCancellation(t *testing.T) {
	for _, stall := range []string{"headers", "body", "upgrade", "cdp"} {
		t.Run(stall, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			closed := make(chan struct{})
			stallRequest := int32(1)
			if stall == "upgrade" || stall == "cdp" {
				stallRequest = 2
			}
			var requests atomic.Int32
			svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n < stallRequest {
					_ = json.NewEncoder(w).Encode(launcher.New())
					return
				}
				if n > stallRequest {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				if stall == "body" {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				if stall == "cdp" {
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					// Complete the WebSocket handshake, then leave CDP unanswered.
					sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
					fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
					rw.Flush()
					go func() {
						_, _ = io.Copy(io.Discard, conn)
						close(closed)
					}()
				}
				close(entered)
				<-release
			}))
			defer svc.Close()
			defer close(release)
			t.Setenv(BrowserServiceEnv, "ws"+strings.TrimPrefix(svc.URL, "http"))
			t.Setenv(BrowserKeyFileEnv, "")
			SetMaxBrowsers(1)
			defer SetMaxBrowsers(0)
			ctx, cancel := context.WithCancel(WithOwner(context.Background(), "alice"))
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := RenderPDF(ctx, "<p>Scratch CV</p>")
				done <- err
			}()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("render failed before reaching the stalled service: %v", err)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled PDF render = %v, want context cancellation", err)
				}
			case <-time.After(time.Second):
				// Let the old implementation finish before restoring shared limits.
				release <- struct{}{}
				<-done
				t.Fatal("PDF render stayed blocked after cancellation")
			}
			if stall == "cdp" {
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("cancelled render left its browser-service connection open")
				}
			}
			// Cancellation must also release this user's turn and the global slot.
			next, stop := context.WithTimeout(WithOwner(context.Background(), "alice"), time.Second)
			defer stop()
			_, err := RenderPDF(next, "<p>Next CV</p>")
			if err == nil || !strings.Contains(err.Error(), "403") {
				t.Fatalf("next render did not reach the service after cancellation: %v", err)
			}
		})
	}
}

func TestRemoteBootstrapReportsHTTPFailure(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "forbidden", code)
			}))
			defer svc.Close()
			t.Setenv(BrowserServiceEnv, svc.URL)
			t.Setenv(BrowserKeyFileEnv, "")
			_, err := RenderPDF(context.Background(), "<p>Scratch CV</p>")
			if err == nil || !strings.Contains(err.Error(), http.StatusText(code)) {
				t.Fatalf("service status %d = %v, want its HTTP failure", code, err)
			}
			if (code == http.StatusUnauthorized || code == http.StatusForbidden) && !strings.Contains(err.Error(), "key") {
				t.Fatalf("authentication failure does not explain the key: %v", err)
			}
		})
	}
}
