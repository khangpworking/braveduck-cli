package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st)
	s.now = func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }
	s.fetch = func(context.Context, string) (scrape.JobPosting, bool, error) {
		return scrape.JobPosting{Title: "Growth Marketer", Company: "Duck Co", Deadline: "2026-12-01"}, true, nil
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func TestAddJobAppliesRulesAndLogs(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	if _, err := s.AddJob(ctx, NewJob{Title: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank title = %v, want ErrInvalid", err)
	}

	j, err := s.AddJob(ctx, NewJob{
		Title: "Data Analyst",
		URL:   "https://www.topcv.vn/viec-lam/x",
		JD:    "Gửi CV về hr@duck.vn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != "saved" || j.ScoreStatus != "pending" || j.DiscoverySource != "topcv" {
		t.Fatalf("defaults not applied: status=%q score=%q source=%q", j.Status, j.ScoreStatus, j.DiscoverySource)
	}
	if j.ContactEmail != "hr@duck.vn" || j.ApplyMethod == "" {
		t.Fatalf("JD not classified: email=%q method=%q", j.ContactEmail, j.ApplyMethod)
	}

	// The CLI path now feeds the activity feed too.
	ev, _ := s.Activity(ctx, 5)
	if len(ev) != 1 || ev[0].Type != "added" {
		t.Fatalf("activity = %+v, want one 'added'", ev)
	}

	if _, err := s.AddJob(ctx, NewJob{Title: "dup", URL: "https://www.topcv.vn/viec-lam/x"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate URL = %v, want ErrDuplicate", err)
	}
}

func TestAddFromContent(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	j, err := s.AddFromContent(ctx, "https://www.vietnamworks.com/job/1")
	if err != nil {
		t.Fatal(err)
	}
	if j.Title != "Growth Marketer" || j.ExpiresAt != "expire:2026-12-01" || j.DiscoverySource != "vietnamworks" {
		t.Fatalf("extract not applied: %+v", j)
	}

	fetched := false
	s.fetch = func(context.Context, string) (scrape.JobPosting, bool, error) {
		fetched = true
		return scrape.JobPosting{}, false, nil
	}
	if _, err := s.AddFromContent(ctx, "https://www.vietnamworks.com/job/1"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("re-add = %v, want ErrDuplicate", err)
	}
	if fetched {
		t.Fatal("duplicate URL was fetched before being rejected")
	}

	// Links from other sites (and internal addresses) are refused unfetched.
	for _, link := range []string{"https://www.linkedin.com/jobs/view/1", "http://127.0.0.1:6379/"} {
		_, err := s.AddFromContent(ctx, link)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "TopCV") {
			t.Fatalf("%s = %v, want ErrInvalid naming the supported sites", link, err)
		}
	}
	if fetched {
		t.Fatal("an unsupported link was fetched")
	}

	// A blocked page falls back to the text pasted under the link.
	j, err = s.AddFromContent(ctx, "https://www.topcv.vn/viec-lam/x/9.html?ta_source=list\nData Analyst\nMoMo")
	if err != nil || j.Title != "Data Analyst" || j.Company != "MoMo" || j.URL != "https://www.topcv.vn/viec-lam/x/9.html" {
		t.Fatalf("fallback to pasted text: %v %+v", err, j)
	}

	if _, err := s.AddFromContent(ctx, "hi"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too-short paste = %v, want ErrInvalid", err)
	}
}

func TestUpdateJobValidatesAndLogsMoves(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "X"})

	if _, err := s.SetStatus(ctx, j.ID, "banana"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad stage = %v, want ErrInvalid", err)
	}
	got, err := s.SetStatus(ctx, j.ID, " Applied ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "applied" {
		t.Fatalf("status = %q, want normalized 'applied'", got.Status)
	}
	got, err = s.UpdateJob(ctx, j.ID, Patch{Notes: ptr("call back Monday")})
	if err != nil || got.Notes != "call back Monday" || got.Status != "applied" {
		t.Fatalf("notes patch: %v %+v", err, got)
	}

	ev, _ := s.Activity(ctx, 5)
	if len(ev) != 2 || ev[0].Type != "moved" {
		t.Fatalf("activity = %+v, want added then moved (and no event for a notes edit)", ev)
	}

	if _, err := s.SetStatus(ctx, 999, "applied"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing job = %v, want ErrNotFound", err)
	}
}

func TestTogglesAndDelete(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "X"})

	if got, _ := s.TogglePin(ctx, j.ID); !got.Pinned {
		t.Fatal("pin didn't toggle on")
	}
	if got, _ := s.ToggleKeep(ctx, j.ID); !got.Keep || !got.Pinned {
		t.Fatal("keep didn't toggle on independently")
	}
	if _, err := s.TogglePin(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("toggle missing = %v", err)
	}

	del, err := s.DeleteJob(ctx, j.ID)
	if err != nil || del.Title != "X" {
		t.Fatalf("delete: %v %+v", err, del)
	}
	if _, err := s.DeleteJob(ctx, j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete = %v, want ErrNotFound", err)
	}
}

func TestSetRatingConfigInvalidIsErrInvalid(t *testing.T) {
	s := newSvc(t)
	_, err := s.SetRatingConfig(context.Background(), rating.Config{Models: []string{"a"}, DefaultModel: "b"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestStats(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	for _, stage := range []string{"saved", "interview", "closed", "closed"} {
		j, _ := s.AddJob(ctx, NewJob{Title: stage})
		if _, err := s.SetStatus(ctx, j.ID, stage); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 4 || st.ByStage["closed"] != 2 || st.ByStage["offer"] != 0 {
		t.Fatalf("counts wrong: %+v", st)
	}
	if st.Resolved != 3 || st.Interviews != 1 || st.Morale == "" {
		t.Fatalf("rate wrong: %+v", st)
	}
}

func TestReportCrawl(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if err := s.ReportCrawl(ctx, "Face Book", true, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad source name = %v", err)
	}
	if err := s.ReportCrawl(ctx, "Facebook", true, ""); err != nil {
		t.Fatal(err)
	}
	v, _ := s.CrawlStatus(ctx)
	if len(v.Sources) != 1 || v.Sources[0].Name != "facebook" || !v.Sources[0].Healthy {
		t.Fatalf("crawl status = %+v", v)
	}

	// The feed gets one line when a source starts failing, not one per poll.
	s.ReportCrawl(ctx, "facebook", false, "logged out")
	s.ReportCrawl(ctx, "facebook", false, "logged out")
	ev, _ := s.Activity(ctx, 10)
	n := 0
	for _, e := range ev {
		if e.Type == "error" {
			n++
		}
	}
	if n != 1 || !strings.Contains(ev[0].Message, "facebook crawl failed: logged out") {
		t.Fatalf("feed = %+v, want exactly one failure line", ev)
	}
}

func TestEditDetails(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "Old", Company: "Group 1329241984892387"})
	got, err := s.UpdateJob(ctx, j.ID, Patch{Company: ptr("  Việc làm AI Engineer -Việt Nam "), Salary: ptr("20-30 triệu")})
	if err != nil || got.Company != "Việc làm AI Engineer -Việt Nam" || got.Salary != "20-30 triệu" || got.Title != "Old" {
		t.Fatalf("edit = %+v, %v", got, err)
	}
	if _, err := s.UpdateJob(ctx, j.ID, Patch{Title: ptr("   ")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank title = %v", err)
	}
	if _, err := s.UpdateJob(ctx, j.ID, Patch{Company: ptr(strings.Repeat("x", 201))}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too-long company = %v", err)
	}
}

func TestAddFromContentFacebook(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.fetch = func(context.Context, string) (scrape.JobPosting, bool, error) {
		t.Fatal("a Facebook link was fetched")
		return scrape.JobPosting{}, false, nil
	}
	fj := &leadJev{}
	s.SetJev(fj)

	post := "https://m.facebook.com/groups/1/permalink/2/?__cft__[0]=AZ\n" +
		"Cẩm nang AI Engineer — tuyển gấp\n📍 HCM\nGửi CV về hr@duck.vn"
	j, err := s.AddFromContent(ctx, post)
	if err != nil {
		t.Fatal(err)
	}
	if j.URL != "https://www.facebook.com/groups/1/posts/2/" || j.DiscoverySource != "facebook" ||
		j.Title != "Cẩm nang AI Engineer" || j.Company != "" || j.ContactEmail != "hr@duck.vn" {
		t.Fatalf("pasted post: %+v", j)
	}
	// The user chose it, so Jev's lead check doesn't close it.
	if j.Status != "saved" || fj.calls != 0 {
		t.Fatalf("status %q after %d lead-check calls, want saved and none", j.Status, fj.calls)
	}

	// fb-watcher's copy of the same post is the same job.
	if _, err := s.AddFromContent(ctx, "https://www.facebook.com/groups/1/posts/2"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("same post again = %v, want ErrDuplicate", err)
	}

	// Just the link: a card to fill in later.
	j, err = s.AddFromContent(ctx, "https://www.facebook.com/groups/1/posts/3/")
	if err != nil || j.Title != "Facebook post" {
		t.Fatalf("bare link: %v %+v", err, j)
	}
}

func TestAddFromContentRejectsDuplicates(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	post := "HIRING AI ENGINEER at a fintech startup, remote friendly, send your CV to hr@duck.vn"
	// Stored the way fb-watcher adds a post: its text, then a footer.
	watched, _ := s.AddJob(ctx, NewJob{Title: "HIRING AI ENGINEER", Company: "AI Jobs VN", Source: "facebook",
		URL: "https://www.facebook.com/groups/1/posts/2/", JD: post + "\n\n---\nAuthor: Vy\nGroup: AI Jobs VN (https://www.facebook.com/groups/1/)\nMatched keywords: ai"})
	legacy, _ := s.AddJob(ctx, NewJob{Title: "Growth Marketer", URL: "https://www.topcv.vn/viec-lam/x/1.html?ta_source=list"})
	analyst, _ := s.AddJob(ctx, NewJob{Title: "Data Analyst", Company: "MoMo"})

	for _, tc := range []struct {
		name, paste string
		want        jobs.Job
	}{
		{"link stored with tracking parts", "https://www.topcv.vn/viec-lam/x/1.html", legacy},
		{"same title at the same company", "data analyst\nMOMO\nSalary: $2,000", analyst},
		{"same post text under another link", "https://www.facebook.com/groups/1/posts/999/\n" + post, watched},
		{"same post text on its own", "  " + strings.ToLower(post), watched},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.AddFromContent(ctx, tc.paste)
			var dup *DuplicateError
			if !errors.Is(err, ErrDuplicate) || !errors.As(err, &dup) || len(dup.Jobs) != 1 || dup.Jobs[0].ID != tc.want.ID {
				t.Fatalf("err = %v, want a duplicate of #%d", err, tc.want.ID)
			}
			if want := fmt.Sprintf("#%d %q (saved)", tc.want.ID, tc.want.Title); !strings.Contains(err.Error(), want) {
				t.Fatalf("message %q doesn't name %s", err, want)
			}
		})
	}

	// fb-watcher seeing a post the user pasted first is refused too.
	pasted, _ := s.AddFromContent(ctx, "https://www.facebook.com/groups/1/posts/7/\nKế toán tổng hợp, làm việc tại quận 1, lương 15 triệu, gửi CV về ketoan@duck.vn")
	_, err := s.AddJob(ctx, NewJob{Title: "Kế toán tổng hợp", Company: "AI Jobs VN", Source: "facebook", URL: "https://www.facebook.com/groups/1/posts/7/"})
	var dup *DuplicateError
	if !errors.As(err, &dup) || dup.Jobs[0].ID != pasted.ID {
		t.Fatalf("fb-watcher re-add = %v, want a duplicate of #%d", err, pasted.ID)
	}

	// Same title elsewhere, or a short generic paste, is a new job.
	for _, paste := range []string{"Data Analyst\nZaloPay", "HIRING AI ENGINEER"} {
		if _, err := s.AddFromContent(ctx, paste); err != nil {
			t.Fatalf("%q: %v, want added", paste, err)
		}
	}
	if list, _ := s.ListJobs(ctx, ListFilter{}); len(list) != 6 {
		t.Fatalf("board has %d jobs, want the 4 originals + 2 new", len(list))
	}
}

// The title rule only stands in for a missing link: two links are two
// listings, and a Closed job doesn't block a new posting of the same role.
func TestDuplicateTitleRuleNeedsAMissingLink(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	old, _ := s.AddJob(ctx, NewJob{Title: "Backend Developer", Company: "Duck Co", URL: "https://www.topcv.vn/viec-lam/backend/1.html"})
	if _, err := s.AddJob(ctx, NewJob{Title: "Backend Developer", Company: "Duck Co", URL: "https://www.topcv.vn/viec-lam/backend/2.html"}); err != nil {
		t.Fatalf("a second listing with its own link: %v, want added", err)
	}
	if _, err := s.AddJob(ctx, NewJob{Title: "backend developer", Company: "duck co"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("the same role pasted without a link = %v, want ErrDuplicate", err)
	}
	s.SetStatus(ctx, old.ID, "closed")
	c, _ := s.AddJob(ctx, NewJob{Title: "Data Analyst", Company: "MoMo"})
	s.SetStatus(ctx, c.ID, "closed")
	if _, err := s.AddJob(ctx, NewJob{Title: "Data Analyst", Company: "MoMo"}); err != nil {
		t.Fatalf("re-adding a role whose old card is Closed: %v, want added", err)
	}
}

func TestPostTextStripsOnlyAMetadataFooter(t *testing.T) {
	intro := strings.Repeat("Duck Co builds payments for Vietnam. ", 3)
	if got := postText(intro + "\n---\nAuthor: Vy\nGroup: AI Jobs VN (https://fb.com/groups/1/)\nMatched keywords: ai"); got != intro {
		t.Fatalf("fb-watcher footer kept: %q", got)
	}
	jd := intro + "\n---\nResponsibilities\n- build the ledger\n- review PRs"
	if got := postText(jd); got != jd {
		t.Fatalf("a description's --- divider was cut: %q", got)
	}
}
