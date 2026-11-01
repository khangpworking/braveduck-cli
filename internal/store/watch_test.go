package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// The board's server and `braveduck mcp` are separate processes with separate
// handles on one file; a write through either must wake the other's watcher.
func TestWatchSeesWritesFromAnotherHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bd.db")
	board, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer board.Close()
	agent, err := Open(path) // stands in for another process
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := board.Watch(ctx, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
		t.Fatal("change reported before any write")
	case <-time.After(100 * time.Millisecond):
	}

	if _, err := agent.Add(ctx, jobs.Job{Title: "from the agent"}, now); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("write from another handle not seen")
	}

	// Writes through the watched store's own pool count too.
	if _, err := board.Add(ctx, jobs.Job{Title: "from the board"}, now); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("write from the same store not seen")
	}

	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			// drain a coalesced value, then expect close
			if _, ok := <-ch; ok {
				t.Fatal("channel not closed after cancel")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestWatchMemoryUnsupported(t *testing.T) {
	s := open(t)
	if _, err := s.Watch(context.Background(), time.Second); !errors.Is(err, ErrWatchUnsupported) {
		t.Fatalf("Watch on :memory: = %v", err)
	}
}
