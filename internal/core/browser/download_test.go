package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A mirror that stalls mid-way must not lose progress: the next attempt
// resumes with a Range request and the result still has to match the checksum.
func TestDownloadResumesAfterStall(t *testing.T) {
	orig := stallTimeout
	stallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { stallTimeout = orig })

	payload := bytes.Repeat([]byte("chromium!"), 50_000) // ~450 KB
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])

	var requests, ranged atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if r.Header.Get("Range") != "" {
			ranged.Add(1)
		}
		if n == 1 {
			// First attempt: send a third of the file, then go silent.
			w.Header().Set("Content-Length", "450000")
			w.WriteHeader(http.StatusOK)
			w.Write(payload[:len(payload)/3])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "a.tar.gz", time.Time{}, bytes.NewReader(payload)) // honours Range
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "a.tar.gz.part")
	if err := fetchResumable(context.Background(), []string{srv.URL}, dst, want); err != nil {
		t.Fatalf("fetchResumable: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, payload) {
		t.Fatalf("file differs (%d vs %d bytes)", len(got), len(payload))
	}
	if ranged.Load() == 0 {
		t.Fatal("second attempt didn't resume with a Range request")
	}
}

// A corrupted download must never be accepted: after a mismatch the partial
// is discarded and refetched from scratch, and a persistent mismatch fails.
func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	payload := []byte("the real archive")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a", time.Time{}, bytes.NewReader([]byte("tampered archive")))
	}))
	defer srv.Close()
	sum := sha256.Sum256(payload)
	dst := filepath.Join(t.TempDir(), "a.part")
	if err := fetchResumable(context.Background(), []string{srv.URL}, dst, hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("tampered archive accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("tampered partial left on disk")
	}
}

// A partial from an earlier run is continued, not thrown away.
func TestDownloadContinuesEarlierPartial(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 10_000)
	sum := sha256.Sum256(payload)
	var servedFrom atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			servedFrom.Store(1)
		}
		http.ServeContent(w, r, "a", time.Time{}, bytes.NewReader(payload))
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "a.part")
	os.WriteFile(dst, payload[:4000], 0o644) // left by an interrupted run
	if err := fetchResumable(context.Background(), []string{srv.URL}, dst, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if servedFrom.Load() != 1 {
		t.Fatal("earlier partial wasn't resumed")
	}
}
