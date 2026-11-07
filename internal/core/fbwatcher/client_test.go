package fbwatcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The client sends the key and the link, reads a post, and turns a refusal
// into an Error with its kind and when to try again.
func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k3y" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["url"] == "https://www.facebook.com/groups/1/posts/2/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": 1, "url": in["url"], "group_name": "G", "text": "hello"})
			return
		}
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": 1, "error": "rate_limited", "message": "slow down"})
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Key: "k3y"}

	p, err := c.FetchPost(context.Background(), "https://www.facebook.com/groups/1/posts/2/")
	if err != nil || p.Text != "hello" || p.GroupName != "G" {
		t.Fatalf("FetchPost = %+v %v", p, err)
	}
	_, err = c.CheckGroup(context.Background(), "https://www.facebook.com/groups/9")
	var e *Error
	if !errors.As(err, &e) || e.Kind != "rate_limited" || e.Status != 429 || e.RetryAfter != 2*time.Minute {
		t.Fatalf("refusal = %#v", err)
	}
	if _, err := (&Client{URL: srv.URL, Key: "wrong"}).FetchPost(context.Background(), "x"); !errors.As(err, &e) || e.Status != 401 {
		t.Errorf("wrong key = %v", err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("BRAVEDUCK_FBWATCHER_URL", "")
	if c, err := FromEnv(); c != nil || err != nil {
		t.Errorf("unset = %v %v", c, err)
	}
	t.Setenv("BRAVEDUCK_FBWATCHER_URL", "http://127.0.0.1:8091/")
	t.Setenv("BRAVEDUCK_FBWATCHER_KEY_FILE", "")
	if _, err := FromEnv(); err == nil {
		t.Error("a URL without a key file was accepted")
	}
}
