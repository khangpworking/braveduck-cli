package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var qs = map[string]Question{"fit": {Type: "choice", Instructions: "Fit?", Criteria: map[string]string{"A": "great", "F": "poor"}}}

func TestAsk(t *testing.T) {
	var auth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"answers":{"fit":{"choice":"A","probabilities":{"A":0.8,"F":0.2}}}}`))
	}))
	defer srv.Close()
	c := New("sk-test-123")
	c.Endpoint = srv.URL
	a, err := c.Ask(context.Background(), "a job", qs)
	if err != nil || a["fit"].Choice != "A" || a["fit"].Confidence() != 0.8 {
		t.Fatalf("ask = %+v, %v", a, err)
	}
	if auth != "Bearer sk-test-123" || got["model"] != DefaultModel || got["state"] != "a job" {
		t.Fatalf("request: auth %q body %v", auth, got)
	}
}

func TestAskRejectsBadAnswersAndRedactsKey(t *testing.T) {
	for name, body := range map[string]string{
		"no answers":     `{}`,
		"unknown choice": `{"answers":{"fit":{"choice":"Z","probabilities":{"A":0.5,"F":0.5}}}}`,
		"bad sum":        `{"answers":{"fit":{"choice":"A","probabilities":{"A":0.9,"F":0.5}}}}`,
		"missing option": `{"answers":{"fit":{"choice":"A","probabilities":{"A":1}}}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
		c := New("k")
		c.Endpoint = srv.URL
		if _, err := c.Ask(context.Background(), "x", qs); err == nil {
			t.Errorf("%s accepted", name)
		}
		srv.Close()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New("sk-secret-999")
	c.Endpoint = srv.URL
	_, err := c.Ask(context.Background(), "x", qs)
	if err == nil || strings.Contains(err.Error(), "sk-secret-999") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v (must say 401 and never contain the key)", err)
	}
}

func TestLoadKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TYPESAFE_API_KEY", "")
	if _, _, err := LoadKey(); err != ErrNoKey {
		t.Fatalf("no key = %v", err)
	}
	os.MkdirAll(filepath.Join(home, ".config/jev-route"), 0o700)
	os.WriteFile(filepath.Join(home, ".config/jev-route/typesafe.env"), []byte("# c\nTYPESAFE_API_KEY='from-file'\n"), 0o600)
	if k, src, err := LoadKey(); err != nil || k != "from-file" || !strings.HasSuffix(src, "typesafe.env") {
		t.Fatalf("file key = %q %q %v", k, src, err)
	}
	t.Setenv("TYPESAFE_API_KEY", "from-env")
	if k, src, _ := LoadKey(); k != "from-env" || src != "TYPESAFE_API_KEY" {
		t.Fatalf("env key = %q %q", k, src)
	}
}
