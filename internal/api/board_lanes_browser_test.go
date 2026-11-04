//go:build browser

package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// On a desktop Closed and empty lanes start folded to a narrow strip (the
// count still shows); a header click unfolds one and is remembered, and Show
// on a closed job unfolds its lane. Phones keep the accordion instead.
func TestBoardFoldsLanes(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	svc.AddJob(ctx, service.NewJob{Title: "Open job"})
	done, _ := svc.AddJob(ctx, service.NewJob{Title: "Old job"})
	svc.SetStatus(ctx, done.ID, "closed")
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(1440, 900, 1, false)
	page.MustNavigate(ts.URL).MustWaitLoad()
	page.MustElement(".card")
	folded := func() bool {
		return page.MustEval(`() => document.getElementById("col-Closed").classList.contains("folded")`).Bool()
	}
	if !folded() || page.MustHas(`#col-Closed .card`) && page.MustElement(`#col-Closed .card`).MustVisible() {
		t.Fatal("Closed isn't folded on a desktop by default")
	}
	if n := page.MustElement("#col-Closed .column-count").MustText(); n != "1" {
		t.Fatalf("folded lane count = %q", n)
	}
	if page.MustEval(`() => document.getElementById("col-Saved").classList.contains("folded")`).Bool() {
		t.Fatal("Saved (with a job) folded by default")
	}
	if !page.MustEval(`() => document.getElementById("col-Interview").classList.contains("folded")`).Bool() {
		t.Fatal("an empty lane isn't folded by default")
	}

	page.MustElement("#col-Closed .column-header").MustEval(`() => this.click()`)
	if folded() {
		t.Fatal("header click didn't unfold Closed")
	}
	page.MustReload().MustWaitLoad()
	page.MustElement(".card")
	if folded() {
		t.Fatal("unfolding wasn't remembered")
	}

	// Fold it again, then Show the closed job: its lane unfolds.
	page.MustElement("#col-Closed .column-header").MustEval(`() => this.click()`)
	page.MustEval(`id => showJobCard(String(id))`, done.ID)
	if folded() || !page.MustElement(fmt.Sprintf(`#col-Closed .card[data-id="%d"]`, done.ID)).MustVisible() {
		t.Fatal("Show didn't reveal the job in a folded lane")
	}

	page.MustSetViewport(390, 844, 2, true)
	page.MustEval(`() => renderBoard()`)
	if page.MustHas(".column.folded") {
		t.Fatal("a lane is folded on a phone")
	}
}
