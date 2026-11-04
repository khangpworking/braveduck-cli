package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func TestEventsStreamsChangesFromOtherProcesses(t *testing.T) {
	watchEvery = 20 * time.Millisecond
	path := filepath.Join(t.TempDir(), "bd.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(Handler(st, service.New(st), testToken))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events?token="+testToken, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	// An agent writes through its own handle, as `braveduck mcp` would.
	agent, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	time.Sleep(100 * time.Millisecond) // let the watcher take its baseline
	if _, err := agent.Add(ctx, jobs.Job{Title: "from the agent"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream closed before a change event")
			}
			if l == "event: changed" {
				return
			}
		case <-deadline:
			t.Fatal("no change event within 3s")
		}
	}
}

func TestEventsNeedToken(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/events?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad query token = %d, want 401", resp.StatusCode)
	}
	// The query-string token is accepted on the event stream only.
	resp, err = http.Get(ts.URL + "/api/jobs?token=" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("query token on /api/jobs = %d, want 401", resp.StatusCode)
	}
}
