package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// connect starts the server and returns a client session talking to it over
// in-memory transports — the same protocol path Claude Code uses over stdio.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ct, sTr := mcp.NewInMemoryTransports()
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ss, err := NewServer(svc).Connect(ctx, sTr, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); ss.Close(); st.Close() })
	return cs
}

// call invokes a tool and decodes its structured output into out (may be nil).
// It returns the result so callers can check IsError.
func call(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if out != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, b, err)
		}
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestListsEveryTool(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
	}
	for _, name := range []string{
		"list_jobs", "get_job", "add_job", "update_job", "delete_job", "set_status",
		"pin_job", "keep_job", "get_keywords", "get_rating_config", "set_rating_config",
		"get_stats", "get_setup", "get_today", "get_crawl_status", "get_activity",
		"get_profile", "update_profile", "upload_cv", "get_rating_queue", "rate_job", "submit_rating",
		"list_platforms", "connect_platform", "disconnect_platform",
		"get_crawl_schedule", "set_crawl_schedule",
		"ask_model", "cv_brief", "submit_cv", "draft_brief", "submit_draft", "reject_draft",
		"apply_job", "crawl", "retry_crawl", "connect_platform", "disconnect_platform", "set_keywords",
	} {
		if got[name] == nil {
			t.Errorf("tool %q not listed", name)
		}
	}
	if a := got["delete_job"].Annotations; a == nil || a.DestructiveHint == nil || !*a.DestructiveHint {
		t.Error("delete_job isn't marked destructive")
	}
	if a := got["list_jobs"].Annotations; a == nil || !a.ReadOnlyHint {
		t.Error("list_jobs isn't marked read-only")
	}
}

// The spec's smoke test: add_job → list_jobs, then a lane move persists.
func TestAddListMove(t *testing.T) {
	cs := connect(t)

	var added jobs.Job
	if res := call(t, cs, "add_job", map[string]any{"title": "Data Analyst", "company": "Duck Co"}, &added); res.IsError {
		t.Fatalf("add_job failed: %s", text(res))
	}
	if added.ID == 0 || added.Status != "saved" {
		t.Fatalf("added = %+v", added)
	}

	var list jobsOut
	call(t, cs, "list_jobs", map[string]any{}, &list)
	if len(list.Jobs) != 1 || list.Jobs[0].Title != "Data Analyst" {
		t.Fatalf("list_jobs = %+v", list)
	}

	var moved jobs.Job
	call(t, cs, "set_status", map[string]any{"id": added.ID, "stage": "interview"}, &moved)
	if moved.Status != "interview" {
		t.Fatalf("set_status → %q", moved.Status)
	}
	call(t, cs, "list_jobs", map[string]any{"status": "interview"}, &list)
	if len(list.Jobs) != 1 {
		t.Fatalf("move didn't persist: %+v", list)
	}

	var stats service.Stats
	call(t, cs, "get_stats", map[string]any{}, &stats)
	if stats.ByStage["interview"] != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestUpdateOnlyNamedFields(t *testing.T) {
	cs := connect(t)
	var j jobs.Job
	call(t, cs, "add_job", map[string]any{"title": "X"}, &j)
	call(t, cs, "update_job", map[string]any{"id": j.ID, "notes": "ping Friday", "pinned": true}, &j)
	if j.Notes != "ping Friday" || !j.Pinned || j.Status != "saved" {
		t.Fatalf("update_job = %+v", j)
	}
}

func TestErrorsAreToolErrors(t *testing.T) {
	cs := connect(t)
	for _, tc := range []struct {
		name, want string
		args       map[string]any
	}{
		{"get_job", "list_jobs", map[string]any{"id": 42}},                          // not found, with a hint
		{"set_status", "unknown stage", map[string]any{"id": 1, "stage": "banana"}}, // validation
		{"add_job", "title", map[string]any{}},                                      // nothing to add
		{"add_job", "not both", map[string]any{"content": "https://x.vn/j", "title": "X"}},
		{"ask_model", "not wired", map[string]any{"id": 1}}, // honest stub
		{"crawl", "search queries", map[string]any{"source": "vietnamworks"}},
		{"set_crawl_schedule", "use 0", map[string]any{"source": "vietnamworks", "every_minutes": 7}},
		{"set_keywords", "unknown keyword list", map[string]any{"lists": map[string]any{"nope": []string{"x"}}}},
		{"get_job", "", map[string]any{"id": "abc"}}, // schema validation
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil {
			// Schema violations may surface as protocol errors; either way the
			// call must not succeed.
			continue
		}
		if !res.IsError {
			t.Errorf("%s(%v) succeeded, want a tool error", tc.name, tc.args)
			continue
		}
		if !strings.Contains(text(res), tc.want) {
			t.Errorf("%s(%v) error = %q, want it to mention %q", tc.name, tc.args, text(res), tc.want)
		}
	}
}

func TestDuplicateURLIsToolError(t *testing.T) {
	cs := connect(t)
	args := map[string]any{"title": "A", "url": "https://www.topcv.vn/j/1"}
	call(t, cs, "add_job", args, nil)
	if res := call(t, cs, "add_job", args, nil); !res.IsError || !strings.Contains(text(res), "already on the board") {
		t.Fatalf("duplicate add = %q (IsError=%v)", text(res), res.IsError)
	}
}

func TestRatingConfigRoundTrip(t *testing.T) {
	cs := connect(t)
	var cfg ratingIO
	call(t, cs, "set_rating_config", map[string]any{"models": []string{"a", "b"}, "default_model": "b", "ab_mode": true}, &cfg)
	call(t, cs, "get_rating_config", map[string]any{}, &cfg)
	if cfg.DefaultModel != "b" || !cfg.ABMode || len(cfg.Models) != 2 {
		t.Fatalf("config = %+v", cfg)
	}
	if res := call(t, cs, "set_rating_config", map[string]any{"models": []string{"a"}, "default_model": "zzz", "ab_mode": false}, nil); !res.IsError {
		t.Fatal("invalid config accepted")
	}
}

// The agent flow: empty profile → told which file to fill; filled → queue,
// judge, submit; the default rater's verdict lands on the board badge.
func TestRatingFlow(t *testing.T) {
	cs := connect(t)
	var j jobs.Job
	call(t, cs, "add_job", map[string]any{"title": "Go backend engineer", "jd": "Go, Postgres, Kubernetes"}, &j)

	var p service.Profile
	call(t, cs, "get_profile", map[string]any{}, &p)
	if p.Filled || !strings.Contains(p.Text, "# What I'm looking for") {
		t.Fatalf("fresh profile = %+v", p)
	}
	res := call(t, cs, "get_rating_queue", map[string]any{}, nil)
	if !res.IsError || !strings.Contains(text(res), p.Path) {
		t.Fatalf("queue with empty profile = %q (IsError=%v), want an error naming %s", text(res), res.IsError, p.Path)
	}

	call(t, cs, "update_profile", map[string]any{"text": "Backend engineer, 5 years Go."}, &p)
	if !p.Filled {
		t.Fatalf("profile after update_profile = %+v", p)
	}
	var q service.RatingBrief
	call(t, cs, "get_rating_queue", map[string]any{}, &q)
	if len(q.Jobs) != 1 || q.Jobs[0].Description != "Go, Postgres, Kubernetes" || q.Rubric == "" {
		t.Fatalf("queue = %+v", q)
	}

	var rated jobs.Job
	if res := call(t, cs, "submit_rating", map[string]any{"id": j.ID, "score": 4.6, "reasoning": "Go + Postgres match • 5y meets bar • k8s a plus"}, &rated); res.IsError {
		t.Fatalf("submit_rating: %s", text(res))
	}
	if rated.ScoreLetter != "A" || rated.ScoreStatus != "scored" {
		t.Fatalf("rated = %+v", rated)
	}
	call(t, cs, "get_rating_queue", map[string]any{}, &q)
	if len(q.Jobs) != 0 {
		t.Fatalf("queue after rating = %+v", q.Jobs)
	}

	if res := call(t, cs, "submit_rating", map[string]any{"id": j.ID, "score": 9, "reasoning": "way out of range"}, nil); !res.IsError {
		t.Fatal("score 9 accepted")
	}
}

func TestPlatformTools(t *testing.T) {
	t.Setenv("BRAVEDUCK_PASSPHRASE", "")
	cs := connect(t)
	var out platformsOut
	call(t, cs, "list_platforms", map[string]any{}, &out)
	if len(out.Platforms) != 4 || out.Platforms[0].Status != "not_connected" {
		t.Fatalf("list_platforms = %+v", out)
	}
	if res := call(t, cs, "connect_platform", map[string]any{"platform": "nope"}, nil); !res.IsError {
		t.Fatal("unknown platform accepted")
	}
	var d disconnectOut
	call(t, cs, "disconnect_platform", map[string]any{"platform": "topcv"}, &d)
	if d.Deleted {
		t.Fatal("disconnect reported a deletion with nothing saved")
	}
}

// upload_cv reads a CV file the user names: kept, and its text in the
// profile; replacing the user's own About me needs replace:true.
func TestUploadCVTool(t *testing.T) {
	cs := connect(t)
	dir := t.TempDir()
	cv := filepath.Join(dir, "cv.md")
	os.WriteFile(cv, []byte("# Khang\n\nMarketing AI, three years of n8n automation at Duck Co."), 0o600)

	var got service.BaseCV
	call(t, cs, "upload_cv", map[string]any{"path": cv}, &got)
	if got.Name != "cv.md" || !got.ProfileFilled {
		t.Fatalf("upload = %+v", got)
	}
	res := call(t, cs, "upload_cv", map[string]any{"path": cv}, nil)
	if !res.IsError || !strings.Contains(text(res), "About me") {
		t.Fatalf("second upload = %q (IsError=%v), want the ask-first error", text(res), res.IsError)
	}
	if res := call(t, cs, "upload_cv", map[string]any{"path": cv, "replace": true}, nil); res.IsError {
		t.Fatalf("replace = %q", text(res))
	}
	for _, bad := range []string{filepath.Join(dir, "missing.pdf"), dir} {
		if res := call(t, cs, "upload_cv", map[string]any{"path": bad}, nil); !res.IsError {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestGetSetupTool(t *testing.T) {
	cs := connect(t)
	var st service.SetupStatus
	call(t, cs, "get_setup", map[string]any{}, &st)
	if st.Done || st.AboutMe || st.SearchQueries != 0 {
		t.Fatalf("fresh setup = %+v", st)
	}
	call(t, cs, "set_keywords", map[string]any{"lists": map[string]any{"search_queries": []string{"n8n"}}}, nil)
	call(t, cs, "get_setup", map[string]any{}, &st)
	if st.SearchQueries != 1 {
		t.Fatalf("after set_keywords = %+v", st)
	}
}

func TestListJobsQueryTool(t *testing.T) {
	cs := connect(t)
	call(t, cs, "add_job", map[string]any{"title": "Kỹ Sư AI", "company": "Sao Mai"}, nil)
	call(t, cs, "add_job", map[string]any{"title": "Data Analyst", "company": "MoMo"}, nil)
	var out struct {
		Jobs []jobs.Job `json:"jobs"`
	}
	call(t, cs, "list_jobs", map[string]any{"query": "ky su"}, &out)
	if len(out.Jobs) != 1 || out.Jobs[0].Title != "Kỹ Sư AI" {
		t.Fatalf("list_jobs query = %+v", out.Jobs)
	}
}
