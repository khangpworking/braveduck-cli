package hosted

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const secret = "0123456789abcdef0123456789abcdef-test"

func newServer(t *testing.T, dir string, maxUsers int) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // never the real ~/.braveduck
	t.Setenv("TYPESAFE_API_KEY", "")
	s, err := New(context.Background(), Options{Dir: dir, UserHeader: "Remote-User", Secret: secret, MaxUsers: maxUsers, LogoutURL: "https://auth.example.com/logout"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// do sends a request as the proxy would; user "" leaves the header out.
func do(t *testing.T, h http.Handler, user, secret, method, path, token, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "jobs.example.com" // the public name, as the proxy passes it on
	if secret != "" {
		r.Header.Set(SecretHeader, secret)
	}
	if user != "" {
		r.Header.Set("Remote-User", user)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	return w.Code, string(b)
}

var tokenRE = regexp.MustCompile(`name="braveduck-token" content="([0-9a-f]+)"`)

// tokenOf loads user's board page and reads its API token, like the browser.
func tokenOf(t *testing.T, h http.Handler, user string) string {
	t.Helper()
	code, page := do(t, h, user, secret, "GET", "/", "", "")
	m := tokenRE.FindStringSubmatch(page)
	if code != http.StatusOK || m == nil {
		t.Fatalf("board page for %s: %d, token found: %v", user, code, m != nil)
	}
	return m[1]
}

func TestOnlyTheProxyGetsIn(t *testing.T) {
	s := newServer(t, t.TempDir(), 5)
	for _, tc := range []struct {
		name, user, secret string
		want               int
	}{
		{"no secret", "alice", "", http.StatusForbidden},
		{"wrong secret", "alice", strings.Repeat("x", 40), http.StatusForbidden},
		{"no user", "", secret, http.StatusUnauthorized},
		{"path in the name", "../alice", secret, http.StatusForbidden},
		{"hidden dir", ".alice", secret, http.StatusForbidden},
		{"logged in", "alice", secret, http.StatusOK},
	} {
		if code, _ := do(t, s, tc.user, tc.secret, "GET", "/", "", ""); code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, code, tc.want)
		}
	}
	if code, _ := do(t, s, "", "", "GET", "/healthz", "", ""); code != http.StatusOK {
		t.Errorf("healthz = %d", code)
	}
	if got := s.Users(); !slices.Equal(got, []string{"alice"}) {
		t.Errorf("users = %v, want only alice (refused requests open nothing)", got)
	}
}

func TestUsersSeeOnlyTheirOwnBoard(t *testing.T) {
	dir := t.TempDir()
	s := newServer(t, dir, 5)
	alice, bob := tokenOf(t, s, "Alice"), tokenOf(t, s, "bob")
	if alice == bob {
		t.Fatal("two users got the same API token")
	}

	if code, body := do(t, s, "alice", secret, "POST", "/api/jobs", alice,
		`{"content":"Senior Go Developer at Duck Co\nHà Nội, 30 triệu"}`); code != http.StatusCreated {
		t.Fatalf("alice adds a job: %d %s", code, body)
	}
	count := func(user, tok string) int {
		code, body := do(t, s, user, secret, "GET", "/api/jobs", tok, "")
		var list []map[string]any
		if code != http.StatusOK || json.Unmarshal([]byte(body), &list) != nil {
			t.Fatalf("%s lists jobs: %d %s", user, code, body)
		}
		return len(list)
	}
	if a, b := count("ALICE", alice), count("bob", bob); a != 1 || b != 0 {
		t.Fatalf("alice sees %d jobs, bob sees %d; want 1 and 0", a, b)
	}
	// One user's token is useless on another user's board.
	if code, _ := do(t, s, "bob", secret, "GET", "/api/jobs", alice, ""); code != http.StatusUnauthorized {
		t.Fatalf("bob with alice's token: %d, want 401", code)
	}

	code, body := do(t, s, "bob", secret, "GET", "/api/setup", bob, "")
	if code != http.StatusOK || !strings.Contains(body, `"hosted":{"user":"bob","logout_url":"https://auth.example.com/logout"}`) {
		t.Fatalf("setup = %d %s", code, body)
	}
	// A window on the server would help nobody.
	if code, body := do(t, s, "bob", secret, "POST", "/api/platforms/topcv/login", bob, ""); code != http.StatusBadRequest || !strings.Contains(body, "shared server") {
		t.Fatalf("login window = %d %s", code, body)
	}

	for _, u := range []string{"alice", "bob"} {
		fi, err := os.Stat(filepath.Join(dir, u, "braveduck.db"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s's database: %v %v", u, fi, err)
		}
	}
}

func TestFullServerAndRestart(t *testing.T) {
	dir := t.TempDir()
	s := newServer(t, dir, 2)
	tokenOf(t, s, "alice")
	tokenOf(t, s, "bob")
	if code, body := do(t, s, "carol", secret, "GET", "/", "", ""); code != http.StatusServiceUnavailable || !strings.Contains(body, "no room") {
		t.Fatalf("third user on a 2-user server: %d %s", code, body)
	}
	tokenOf(t, s, "alice") // existing users still get in
	s.Close()

	if err := os.Mkdir(filepath.Join(dir, "not a user!"), 0o700); err != nil {
		t.Fatal(err)
	}
	s2 := newServer(t, dir, 2)
	if got := s2.Users(); !slices.Equal(got, []string{"alice", "bob"}) {
		t.Fatalf("after a restart: %v, want alice and bob opened", got)
	}
}

func TestOptionsAreChecked(t *testing.T) {
	ok := Options{Dir: t.TempDir(), UserHeader: "Remote-User", Secret: secret, MaxUsers: 1}
	for name, mod := range map[string]func(*Options){
		"no dir":       func(o *Options) { o.Dir = "" },
		"no header":    func(o *Options) { o.UserHeader = "" },
		"short secret": func(o *Options) { o.Secret = "short" },
		"no users":     func(o *Options) { o.MaxUsers = 0 },
	} {
		o := ok
		mod(&o)
		if _, err := New(context.Background(), o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
