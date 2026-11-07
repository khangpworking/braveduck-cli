//go:build browser

package api

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
	"github.com/khangpworking/braveduck-cli/internal/core/geo"
)

// launchBrowser starts headless Chromium (CLOAKBROWSER_BINARY_PATH if set).
func launchBrowser(t *testing.T) *rod.Browser {
	t.Helper()
	l := launcher.New().Headless(true).Set("no-sandbox")
	if bin := os.Getenv("CLOAKBROWSER_BINARY_PATH"); bin != "" {
		l = l.Bin(bin)
	}
	// Every page op gets a deadline, so a stuck step fails this test with a
	// timeout error instead of hanging the whole suite.
	b := rod.New().ControlURL(l.MustLaunch()).MustConnect().Timeout(60 * time.Second)
	// Close, then kill the process and remove its profile: otherwise every
	// test's Chromium keeps running underneath the next test.
	t.Cleanup(func() { _ = b.CancelTimeout().Close(); l.Kill(); l.Cleanup() })
	go b.EachEvent(func(e *proto.PageJavascriptDialogOpening) {
		t.Logf("DIALOG %s: %q", e.Type, e.Message)
	}, func(e *proto.RuntimeExceptionThrown) {
		t.Logf("JS EXCEPTION: %s", e.ExceptionDetails.Exception.Description)
	}, func(e *proto.PageFrameNavigated) {
		t.Logf("NAVIGATED %s", e.Frame.URL)
	}, func(e *proto.TargetTargetCrashed) {
		t.Logf("RENDERER CRASHED")
	})()
	return b
}

func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func must[T any](v T, _ error) T { return v }

// boardFakeGeo finds one place, Landmark 81, so location tests need no network.
type boardFakeGeo struct{}

func (boardFakeGeo) Search(_ context.Context, q string, _ int, _ geo.Point) ([]geo.Place, error) {
	if strings.Contains(q, "Landmark") {
		return []geo.Place{{Point: geo.Point{Lat: 10.7949, Lng: 106.7219}, Label: "Landmark 81, Bình Thạnh"}}, nil
	}
	return nil, geo.ErrNotFound
}

// countingCrawler counts its searches, so a test sees which sites were checked.
type countingCrawler struct {
	id    string
	calls atomic.Int32
}

func (c *countingCrawler) Name() string { return c.id }
func (c *countingCrawler) Search(context.Context, string) ([]crawl.Listing, error) {
	c.calls.Add(1)
	return []crawl.Listing{{Title: "Job at " + c.id, Company: "Duck Co", URL: "https://www." + c.id + ".com/x-1-jv"}}, nil
}
