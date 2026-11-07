//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// actionsBoard is appBoard with a filled-in profile, so briefs for the
// user's AI can be made, and the job ids in list order.
func actionsBoard(t *testing.T, list []jobs.Job) (*store.Store, string, []int64) {
	t.Helper()
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := os.WriteFile(filepath.Join(dir, "profile.md"), []byte("Accountant, 3 years. Wants 12 triệu or more."), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := service.New(st)
	svc.SetDataDir(dir)
	var ids []int64
	for _, j := range list {
		id, err := st.Add(context.Background(), j, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	ts := httptest.NewServer(Handler(st, svc, testToken))
	t.Cleanup(ts.Close)
	return st, ts.URL + AppPath, ids
}

func getJob(t *testing.T, st *store.Store, id int64) jobs.Job {
	t.Helper()
	j, err := st.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func clickText(page *rod.Page, sel, text string) {
	page.MustElementR(sel, text).MustEval(`() => this.click()`)
}

// Following up: copy the message, say it's sent, and Today stops asking.
func TestAppFollowUp(t *testing.T) {
	st, url, ids := actionsBoard(t, []jobs.Job{
		{Title: "Tour accountant", Company: "Sen Việt", Status: "applied", ExpiresAt: "applied:" + appDay(-8)},
	})
	page := launchBrowser(t).MustPage("")
	page.MustSetViewport(1280, 844, 1, false)
	page.MustNavigate(url).MustWaitLoad()
	clickText(page, "button", "Ask Sen Việt for an answer")
	clickText(page, "button", "I’ve sent it")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).FollowupSentAt != "" }, "the follow-up wasn't recorded")
	page.MustElementR("section", "You asked 0 days ago")
	page.MustWait(`() => !document.querySelector('[aria-label="Today"]')`)
}

// The ⋯ menu pins, keeps, edits and deletes; notes save as you type.
func TestAppMoreMenuAndNotes(t *testing.T) {
	st, url, ids := actionsBoard(t, []jobs.Job{
		{Title: "Accountant", Company: "Hoa Sen", Status: "saved", ExpiresAt: "expire:" + appDay(20)},
		{Title: "Cashier", Status: "saved"},
	})
	page := appPage(t, url, 1280)
	page.MustElement("section[aria-label='Cashier']") // newest first among unrated

	menu := func(item string) {
		// Radix opens menus on pointer down, not click.
		page.MustElement(`button[aria-label="More actions"]`).MustEval(`() => this.dispatchEvent(new PointerEvent("pointerdown", {bubbles: true, button: 0, pointerType: "mouse"}))`)
		clickText(page, "[role=menuitemcheckbox], [role=menuitem]", item)
	}
	clickText(page, "[data-job]", "Accountant")
	page.MustElement("section[aria-label='Accountant']")
	menu("Pin to the top")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).Pinned }, "pin didn't save")
	menu("Keep")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).Keep }, "keep didn't save")

	menu("Edit details")
	title := page.MustElementR("label", "^Title").MustElement("input")
	title.MustSelectAllText().MustInput("Chief accountant")
	clickText(page, "[role=dialog] button", "^Save$")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).Title == "Chief accountant" }, "the edit didn't save")

	clickText(page, "button", "My notes")
	page.MustElement(`textarea[aria-label="My notes"]`).MustInput("Call Ms Lan on Monday")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).Notes == "Call Ms Lan on Monday" }, "notes didn't save")

	menu("Delete")
	clickText(page, "[role=alertdialog] button", "^Delete$")
	waitUntil(t, func() bool { _, err := st.Get(context.Background(), ids[0]); return err != nil }, "delete didn't delete")
	page.MustElement("section[aria-label='Cashier']")
}

// The user's AI writes the CV and the email: the board shows what to ask
// for and saves what the AI wrote.
func TestAppTailoredCVAndEmail(t *testing.T) {
	st, url, ids := actionsBoard(t, []jobs.Job{{Title: "Accountant", Company: "Hoa Sen", Status: "saved", URL: "https://example.com/j/1"}})
	page := appPage(t, url, 1280)

	clickText(page, "button", "CV and email for this job")
	clickText(page, "button", "Ask your AI to tailor it")
	page.MustElementR("[role=dialog] pre", "Accountant")
	page.MustElement(`[role=dialog] textarea`).MustInput("# Nguyễn Thị Mai\n\nAccountant with 3 years of experience in general ledger work.")
	clickText(page, "[role=dialog] button", "Save what my AI wrote")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).CVStatus == "ready" }, "the CV wasn't saved")
	page.MustElementR("section", "Version 1")

	clickText(page, "button", "Ask your AI to write it")
	page.MustElement(`[role=dialog] textarea`).MustInput("Subject: Applying for Accountant\n\nDear Hoa Sen, I would like to apply for the accountant role.")
	clickText(page, "[role=dialog] button", "Save what my AI wrote")
	waitUntil(t, func() bool { return strings.Contains(getJob(t, st, ids[0]).DraftEmail, "Applying for Accountant") }, "the email wasn't saved")
	if v := page.MustElementR("label", "^Subject").MustElement("input").MustProperty("value").String(); v != "Applying for Accountant" {
		t.Errorf("subject shows %q", v)
	}
	// With a draft and no site login, sending the email is the main action.
	page.MustElementR("a", "Send from my mail app")
}

// After applying in a site's window, the job asks whether it was submitted.
func TestAppConfirmAssistedApply(t *testing.T) {
	st, url, ids := actionsBoard(t, []jobs.Job{{Title: "Accountant", Status: "saved", URL: "https://example.com/j/1"}})
	if err := st.SetApplyState(context.Background(), ids[0], "confirm", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	page := appPage(t, url, 1280)
	page.MustElementR("p", "Did you submit it")
	if page.MustHasR("button", "Not a fit") {
		t.Error("skip is offered while the board waits for an answer")
	}
	clickText(page, "button", "Yes, I applied")
	waitUntil(t, func() bool { return getJob(t, st, ids[0]).Status == "applied" }, "confirming didn't move the job to Applied")
}
