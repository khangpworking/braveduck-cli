package hosted

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// withKey is an agent's request as the proxy passes it on: the agent's key,
// plus the proxy's secret.
type withKey struct{ key string }

func (a withKey) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(SecretHeader, secret)
	r.Header.Set("Authorization", "Bearer "+a.key)
	return http.DefaultTransport.RoundTrip(r)
}

// agentKey opens user's board and makes their agent key, as on the board.
func agentKey(t *testing.T, s *Server, user string) string {
	t.Helper()
	tn, err := s.user(user)
	if err != nil {
		t.Fatal(err)
	}
	key, err := tn.svc.NewAgentKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func connectMCP(t *testing.T, url, key string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   url + MCPPath,
		HTTPClient: &http.Client{Transport: withKey{key}},
		MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("connect with %q: %v", key, err)
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
	alice, bob := connectMCP(t, ts.URL, agentKey(t, s, "alice")), connectMCP(t, ts.URL, agentKey(t, s, "bob"))

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

// /mcp needs the user's agent key: a login (Remote-User), someone else's
// key, a key with another user's name on it or a revoked key get nothing.
func TestMCPNeedsTheAgentKey(t *testing.T) {
	s := newServer(t, t.TempDir(), 5)
	alice := agentKey(t, s, "alice")
	bob := agentKey(t, s, "bob")
	_, aliceSecret, _ := strings.Cut(alice, ":")
	tn, _ := s.user("carol")
	revoked, _ := tn.svc.NewAgentKey(context.Background())
	tn.svc.RevokeAgentKey(context.Background())
	for name, h := range map[string]map[string]string{
		"logged in, no key":        {"Remote-User": "alice"},
		"no secret":                {"Authorization": "Bearer " + alice, SecretHeader: ""},
		"bob's name, alice's key":  {"Authorization": "Bearer bob:" + aliceSecret},
		"bob's key, alice's login": {"Authorization": "Bearer " + bob + "x", "Remote-User": "alice"},
		"revoked":                  {"Authorization": "Bearer " + revoked},
		"unknown user":             {"Authorization": "Bearer dave:" + aliceSecret},
	} {
		r := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		r.Header.Set(SecretHeader, secret)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range h {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 401/403", name, w.Code)
		}
	}
	if got := s.Users(); slices.Contains(got, "dave") {
		t.Error("an agent key opened a board for an unknown user")
	}
}

// A page alice visits can't drive her agent endpoint from her browser.
func TestMCPRefusesCrossSiteBrowsers(t *testing.T) {
	s := newServer(t, t.TempDir(), 5)
	r := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set(SecretHeader, secret)
	r.Header.Set("Authorization", "Bearer "+agentKey(t, s, "alice"))
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

// With the login service's users file, an agent key stops working as soon
// as its user is disabled or removed there (and if the file is unreadable).
func TestAgentKeyFollowsTheLoginUsers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	usersFile := filepath.Join(t.TempDir(), "users_database.yml")
	write := func(yml string) {
		t.Helper()
		if err := os.WriteFile(usersFile, []byte(yml), 0o600); err != nil {
			t.Fatal(err)
		}
		future := time.Now().Add(time.Duration(len(yml)) * time.Second) // a new mtime every write
		os.Chtimes(usersFile, future, future)
	}
	write("users:\n  alice:\n    disabled: false\n    password: 'x'\n")
	s, err := New(context.Background(), Options{Dir: t.TempDir(), UserHeader: "Remote-User", Secret: secret, MaxUsers: 5, UsersFile: usersFile})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key := agentKey(t, s, "alice")
	call := func() int {
		r := httptest.NewRequest("POST", MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		r.Header.Set(SecretHeader, secret)
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if c := call(); c != http.StatusOK {
		t.Fatalf("active user: %d", c)
	}
	write("users:\n  alice:\n    disabled: true\n    password: 'x'\n")
	if c := call(); c != http.StatusUnauthorized {
		t.Fatalf("disabled user: %d, want 401", c)
	}
	write("users:\n  bob:\n    password: 'y'\n")
	if c := call(); c != http.StatusUnauthorized {
		t.Fatalf("removed user: %d, want 401", c)
	}
	write("users:\n  Alice:\n    password: 'x'\n")
	if c := call(); c != http.StatusOK {
		t.Fatalf("back again: %d", c)
	}
	os.Remove(usersFile)
	if c := call(); c != http.StatusUnauthorized {
		t.Fatalf("no users file: %d, want 401", c)
	}
}
