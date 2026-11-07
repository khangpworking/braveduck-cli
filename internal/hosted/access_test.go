package hosted

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeAccess is a Cloudflare Access team: it publishes its signing key at
// /cdn-cgi/access/certs and signs tokens like the ones Access adds to
// requests.
type fakeAccess struct {
	*httptest.Server
	key *rsa.PrivateKey
}

func newFakeAccess(t *testing.T) *fakeAccess {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAccess{key: key}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}}
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(f.Close)
	return f
}

// token signs claims with key (the team's own key unless another is given).
func (f *fakeAccess) token(t *testing.T, key *rsa.PrivateKey, iss, aud, email string, exp time.Time) string {
	t.Helper()
	if key == nil {
		key = f.key
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{"iss": iss, "aud": []string{aud}, "email": email, "exp": exp.Unix(), "iat": time.Now().Unix()}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func accessServer(t *testing.T, f *fakeAccess, dir string) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // never the real ~/.braveduck
	t.Setenv("TYPESAFE_API_KEY", "")
	// The fake team's certificate is self-signed: fetch its keys with a
	// client that trusts it, the way oidc lets a caller choose the client.
	ctx := oidc.ClientContext(context.Background(), f.Client())
	s, err := New(ctx, Options{Dir: dir, MaxUsers: 5, AccessTeam: f.URL, AccessAUD: "board-aud"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, h http.Handler, path, accessToken string) (int, string) {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "app.example.com"
	if accessToken != "" {
		r.Header.Set(AccessHeader, accessToken)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	return w.Code, string(b)
}

// With Cloudflare Access as the login, only a token Access signed for this
// application gets a board, and the board belongs to the token's email.
func TestCloudflareAccessLogin(t *testing.T) {
	f := newFakeAccess(t)
	dir := t.TempDir()
	s := accessServer(t, f, dir)
	soon := time.Now().Add(time.Hour)

	code, page := get(t, s, "/", f.token(t, nil, f.URL, "board-aud", "Mai.Nguyen@Example.com", soon))
	if code != http.StatusOK || !tokenRE.MatchString(page) {
		t.Fatalf("a valid Access token: %d, want the board", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "mai.nguyen@example.com", "braveduck.db")); err != nil {
		t.Fatalf("the board isn't the email's own: %v", err)
	}

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{
		"no token":         "",
		"another app":      f.token(t, nil, f.URL, "other-aud", "mai@example.com", soon),
		"another team":     f.token(t, nil, "https://evil.cloudflareaccess.com", "board-aud", "mai@example.com", soon),
		"expired":          f.token(t, nil, f.URL, "board-aud", "mai@example.com", time.Now().Add(-time.Hour)),
		"not signed by it": f.token(t, other, f.URL, "board-aud", "mai@example.com", soon),
		"not even a token": "hello",
		"a proxy's header": "",
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if tok != "" {
			r.Header.Set(AccessHeader, tok)
		}
		if name == "a proxy's header" { // the Authelia way doesn't work here
			r.Header.Set("Remote-User", "mai@example.com")
			r.Header.Set(SecretHeader, secret)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, w.Code)
		}
	}

	// The board links to Access's own log-out page.
	tok := tokenRE.FindStringSubmatch(page)[1]
	r := httptest.NewRequest("GET", "/api/setup", nil)
	r.Header.Set(AccessHeader, f.token(t, nil, f.URL, "board-aud", "mai.nguyen@example.com", soon))
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if body, _ := io.ReadAll(w.Result().Body); !strings.Contains(string(body), `"logout_url":"/cdn-cgi/access/logout"`) {
		t.Errorf("setup = %d %s", w.Code, body)
	}

	// AI agents skip Access (a Bypass policy) and need their agent key.
	if code, body := get(t, s, MCPPath, ""); code != http.StatusUnauthorized || !strings.Contains(body, "agent key") {
		t.Errorf("/mcp without a key = %d %s", code, body)
	}
	if code, _ := get(t, s, "/healthz", ""); code != http.StatusOK {
		t.Errorf("/healthz = %d", code)
	}
}

func TestCloudflareAccessOptionsAreChecked(t *testing.T) {
	for _, o := range []Options{
		{Dir: t.TempDir(), MaxUsers: 1, AccessTeam: "myteam.cloudflareaccess.com", AccessAUD: "x"},
		{Dir: t.TempDir(), MaxUsers: 1, AccessTeam: "https://myteam.cloudflareaccess.com"},
	} {
		if _, err := New(context.Background(), o); err == nil {
			t.Errorf("New(%+v) accepted it", o)
		}
	}
}
