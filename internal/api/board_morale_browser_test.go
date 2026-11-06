//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The interview-rate line shows the server's numbers and message (the same
// as get_stats), not a second calculation in the browser.
func TestBoardMoraleFromServer(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	for _, status := range []string{"interview", "closed", "closed", "applied"} {
		st.Add(ctx, jobs.Job{Title: status + " job", Status: status, ExpiresAt: "applied:" + time.Now().Format("2006-01-02")}, time.Now())
	}
	want, _ := svc.Stats(ctx)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	page.MustWait(`() => document.getElementById("moraleRate").textContent !== "—"`)
	page.MustElement("#todayWidget summary").MustEval(`() => this.click()`)
	if got := page.MustElement("#moraleRate").MustText(); got != "33%" {
		t.Fatalf("rate = %q, want 33%% (1 interview of 3 resolved)", got)
	}
	if got := page.MustElement("#moraleSubline").MustText(); !strings.Contains(got, "1 interview from 3 answered applications · 1 waiting") {
		t.Fatalf("subline = %q", got)
	}
	if got := page.MustElement("#moraleMessage").MustText(); got != want.Morale || got == "" {
		t.Fatalf("message = %q, want the server's %q", got, want.Morale)
	}
}
