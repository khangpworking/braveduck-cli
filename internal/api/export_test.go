package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func TestExportCSV(t *testing.T) {
	ts, st := newTestServer(t)
	st.Add(context.Background(), jobs.Job{Title: "Kỹ Sư AI", Company: "Sao Mai"}, time.Now())
	resp := do(t, http.MethodGet, ts.URL+"/api/export.csv", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(string(body), "Kỹ Sư AI,Sao Mai") {
		t.Fatalf("export = %d %v %q", resp.StatusCode, resp.Header, body)
	}
	if resp, _ := http.Get(ts.URL + "/api/export.csv"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}
