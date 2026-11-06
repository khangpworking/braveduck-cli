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

// Pasting a Facebook group's link in + Add Job follows the group: the board
// says so, lists it with its status under the paste box, and can unfollow.
func TestBoardFollowsFacebookGroup(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	defer ts.Close()

	page := launchBrowser(t).MustPage("")
	page.MustNavigate(ts.URL).MustWaitLoad()
	clickID(page, "openDrawerBtn")
	waitVisible(page, "#pasteJDTextarea")
	page.MustElement("#pasteJDTextarea").MustInput("https://m.facebook.com/groups/AI.Jobs.VN/?ref=share")
	clickID(page, "extractAddBtn")
	page.MustWait(`() => document.getElementById("extractResultBanner").textContent.includes("Following this Facebook group")`)
	page.MustWait(`() => !document.getElementById("fbGroupsBlock").hidden`)
	if got := page.MustElement("#fbGroupsList").MustText(); !strings.Contains(got, "facebook.com/groups/ai.jobs.vn") || !strings.Contains(got, "Checking") {
		t.Fatalf("groups list = %q", got)
	}
	if list, _ := svc.ListJobs(context.Background(), service.ListFilter{}); len(list) != 0 {
		t.Fatalf("a group link added a job card: %+v", list)
	}

	// The scanner found it public: the list says so after a re-render.
	svc.SetGroupStatus(context.Background(), "ai.jobs.vn", "AI Jobs VN", service.FBPublic, "")
	page.MustEval(`() => renderFBGroups()`)
	page.MustWait(`() => document.getElementById("fbGroupsList").textContent.includes("AI Jobs VN")`)
	if got := page.MustElement("#fbGroupsList").MustText(); !strings.Contains(got, "Public") {
		t.Fatalf("after the scan: %q", got)
	}

	page.MustElementR("#fbGroupsList button", "Unfollow").MustEval(`() => this.click()`)
	page.MustWait(`() => document.getElementById("fbGroupsBlock").hidden`)
	if gs, _ := svc.FBGroups(context.Background()); len(gs) != 0 {
		t.Fatalf("still following: %+v", gs)
	}
}
