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
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ts := httptest.NewServer(Handler(st))
	t.Cleanup(func() { ts.Close(); st.Close() })
	return ts, st
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

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/jobs/1",
		strings.NewReader(`{"status":"applied","pinned":true}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
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
	resp, err := http.Get(ts.URL + "/api/jobs/999")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing job = %d, want 404", resp.StatusCode)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}
