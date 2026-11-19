package api

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Keys pasted in Settings → Rating are write-only: no reply carries them.
func TestDecisionKeysAreWriteOnly(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	key := make([]byte, 32)
	rand.Read(key)
	os.WriteFile(filepath.Join(dir, "session.key"), key, 0o600) // never the real keychain
	svc := service.New(st)
	svc.SetDataDir(dir)
	ts := httptest.NewServer(Handler(st, svc, testToken))
	t.Cleanup(func() { ts.Close(); st.Close() })

	body := func(r *http.Response) string { b, _ := io.ReadAll(r.Body); return string(b) }
	put := do(t, "PUT", ts.URL+"/api/decision",
		`{"provider":"clef","jev_key":"jev-secret-31","clef_token":"cf-secret-42","clef_account_id":"bd23a6717746b9b4ebb6f4c3c84727c7","clef_model":"clef"}`)
	putBody := body(put)
	if put.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d %s", put.StatusCode, putBody)
	}
	get := do(t, "GET", ts.URL+"/api/decision", "")
	getBody := body(get)
	for _, b := range []string{putBody, getBody} {
		if strings.Contains(b, "secret-31") || strings.Contains(b, "secret-42") {
			t.Fatalf("a key came back: %s", b)
		}
	}
	var got service.DecisionSettings
	if err := json.Unmarshal([]byte(getBody), &got); err != nil {
		t.Fatal(err)
	}
	if got.Provider != "clef" || got.Model != "clef" || !got.Jev.Saved || !got.Clef.Saved || got.ClefModel != "clef" {
		t.Fatalf("GET = %+v", got)
	}

	if r := do(t, "PUT", ts.URL+"/api/decision", `{"clef_account_id":"nope"}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad account ID = %d, want 400", r.StatusCode)
	}
}
