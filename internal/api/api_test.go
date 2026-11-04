package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

const testToken = "test-token"

var storeFilterAll = store.ListFilter{}

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	svc := service.New(st)
	svc.SetDataDir(t.TempDir())
	ts := httptest.NewServer(Handler(st, svc, testToken))
	t.Cleanup(func() { ts.Close(); st.Close() })
	return ts, st
}

// do sends an authenticated request (body may be "") and returns the response.
func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestListAndGet(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()
	id, err := st.Add(ctx, jobs.Job{Title: "Marketing Lead", Company: "Duck Co"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var list []jobs.Job
	getJSON(t, ts.URL+"/api/jobs", &list)
	if len(list) != 1 || list[0].Title != "Marketing Lead" {
		t.Fatalf("unexpected list: %+v", list)
	}

	var one jobs.Job
	getJSON(t, ts.URL+"/api/jobs/1", &one)
	if one.ID != id || one.Company != "Duck Co" {
		t.Fatalf("unexpected job: %+v", one)
	}
}

func TestPatchMovesLaneAndPins(t *testing.T) {
	ts, st := newTestServer(t)
	if _, err := st.Add(context.Background(), jobs.Job{Title: "X"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	resp := do(t, http.MethodPatch, ts.URL+"/api/jobs/1", `{"status":"applied","pinned":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", resp.StatusCode)
	}
	var got jobs.Job
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "applied" || !got.Pinned || got.Keep {
		t.Fatalf("patch didn't apply the named fields only: %+v", got)
	}
}

func TestGetMissingIs404(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/jobs/999", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing job = %d, want 404", resp.StatusCode)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp := do(t, http.MethodGet, url, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestPatchRejectsUnknownStage(t *testing.T) {
	ts, st := newTestServer(t)
	if _, err := st.Add(context.Background(), jobs.Job{Title: "X"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if resp := do(t, http.MethodPatch, ts.URL+"/api/jobs/1", `{"status":"banana"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown stage = %d, want 400", resp.StatusCode)
	}
}

func TestCreateDuplicateIs409(t *testing.T) {
	ts, st := newTestServer(t)
	if _, err := st.Add(context.Background(), jobs.Job{Title: "X", URL: "https://www.topcv.vn/j/1"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	resp := do(t, http.MethodPost, ts.URL+"/api/jobs", `{"content":"https://www.topcv.vn/j/1?utm_source=x"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate = %d, want 409", resp.StatusCode)
	}
	var body struct {
		Error      string `json:"error"`
		Duplicates []struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"duplicates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Duplicates) != 1 || body.Duplicates[0].ID != 1 || body.Duplicates[0].Title != "X" || !strings.Contains(body.Error, "#1") {
		t.Fatalf("409 body = %+v, want it to name job #1", body)
	}
}

func TestCrawlMessage(t *testing.T) {
	for _, tc := range []struct {
		res  service.CrawlResult
		want string
	}{
		{service.CrawlResult{Added: 3}, "3 new job(s) from careerlink"},
		{service.CrawlResult{Added: 0, Duplicates: 7, Filtered: 2}, "0 new job(s) from careerlink · 7 already on the board · 2 filtered out"},
	} {
		if got := crawlMessage("careerlink", tc.res); got != tc.want {
			t.Errorf("crawlMessage(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}
