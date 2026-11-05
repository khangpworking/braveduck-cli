package hosted

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// asUser is what the proxy adds to an agent's requests once it has logged in.
type asUser struct {
	user   string
	secret string
}

func (a asUser) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(SecretHeader, a.secret)
	r.Header.Set("Remote-User", a.user)
	return http.DefaultTransport.RoundTrip(r)
}

func connectMCP(t *testing.T, url, user string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   url + MCPPath,
		HTTPClient: &http.Client{Transport: asUser{user, secret}},
		MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("%s connects: %v", user, err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// Each user's AI agent reaches that user's own board over HTTP, without the
// tools that only work on the user's own computer.
func TestMCPOverHTTPPerUser(t *testing.T) {
	s := newServer(t, t.TempDir(), 5)
	ts := httptest.NewServer(s)
	defer ts.Close()
	ctx := context.Background()
	alice, bob := connectMCP(t, ts.URL, "alice"), connectMCP(t, ts.URL, "bob")

	tools, err := alice.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	for _, gone := range []string{"upload_cv", "connect_platform", "apply_job", "confirm_applied"} {
		if slices.Contains(names, gone) {
			t.Errorf("%s offered on a shared server", gone)
		}
	}
	for _, want := range []string{"list_jobs", "add_job", "get_rating_queue", "submit_rating", "submit_cv"} {
		if !slices.Contains(names, want) {
			t.Errorf("%s missing", want)
		}
	}

	res, err := alice.CallTool(ctx, &mcp.CallToolParams{Name: "add_job", Arguments: map[string]any{
		"content": "Backend Engineer at Duck Co\nHà Nội, Go and Postgres",
	}})
	if err != nil || res.IsError {
		t.Fatalf("alice adds a job: %v %+v", err, res)
	}
	count := func(user string) int {
		tn, err := s.user(user)
		if err != nil {
			t.Fatal(err)
		}
		list, err := tn.svc.ListJobs(ctx, service.ListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	if a, b := count("alice"), count("bob"); a != 1 || b != 0 {
		t.Fatalf("alice has %d jobs, bob %d; want 1 and 0", a, b)
	}
	if res, err := bob.CallTool(ctx, &mcp.CallToolParams{Name: "list_jobs", Arguments: map[string]any{}}); err != nil || res.IsError {
		t.Fatalf("bob lists: %v %+v", err, res)
	} else if text := res.Content[0].(*mcp.TextContent).Text; strings.Contains(text, "Backend Engineer") {
		t.Fatalf("bob's agent sees alice's job: %s", text)
	}
}

// A page alice visits can't drive her agent endpoint with her login cookie.
func TestMCPRefusesCrossSiteBrowsers(t *testing.T) {
	s := newServer(t, t.TempDir(), 5)
	r := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set(SecretHeader, secret)
	r.Header.Set("Remote-User", "alice")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST = %d, want 403", w.Code)
	}
}
