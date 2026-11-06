package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func TestCancelledCVWriteCannotOverwriteNextSubmission(t *testing.T) {
	dir := t.TempDir()
	var stores []*Store
	for range 2 {
		s, err := Open(filepath.Join(dir, "board.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		stores = append(stores, s)
	}
	id, err := stores[0].Add(context.Background(), jobs.Job{Title: "Engineer"}, now)
	if err != nil {
		t.Fatal(err)
	}
	write := func(version int, content string) (string, error) {
		path := filepath.Join(dir, fmt.Sprintf("job-%d-v%d.md", id, version))
		return path, os.WriteFile(path, []byte(content), 0o600)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		_, err := stores[0].SaveCV(ctx, id, 30, now, func(version int) (string, error) {
			close(started)
			<-release // an in-progress local write cannot be interrupted by context cancellation
			return write(version, "cancelled submission")
		})
		first <- err
	}()
	select {
	case <-started:
	case err := <-first:
		t.Fatalf("first submission never reached its write: %v", err)
	}
	cancel()
	type result struct {
		job jobs.Job
		err error
	}
	second := make(chan result, 1)
	go func() {
		j, err := stores[1].SaveCV(context.Background(), id, 30, now, func(version int) (string, error) {
			return write(version, "accepted submission")
		})
		second <- result{j, err}
	}()
	// Give the second writer a chance to finish while the cancelled write is
	// still pending. It must not reuse the version until that write returns.
	var got result
	select {
	case got = <-second:
		close(release)
	case <-time.After(100 * time.Millisecond):
		close(release)
		got = <-second
	}
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled submission = %v", err)
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	data, err := os.ReadFile(got.job.CVPath)
	if err != nil || string(data) != "accepted submission" {
		t.Fatalf("accepted CV was overwritten: %q, %v", data, err)
	}
}
