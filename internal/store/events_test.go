package store

import (
	"context"
	"fmt"
	"testing"
)

func TestPruneEvents(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		s.LogEvent(ctx, "added", fmt.Sprintf("event %d", i), now)
	}
	if n, err := s.PruneEvents(ctx, 5); err != nil || n != 7 {
		t.Fatalf("pruned %d (%v), want 7", n, err)
	}
	ev, _ := s.RecentEvents(ctx, 100)
	if len(ev) != 5 || ev[0].Message != "event 11" || ev[4].Message != "event 7" {
		t.Fatalf("kept %+v, want the newest 5", ev)
	}
	if n, _ := s.PruneEvents(ctx, 5); n != 0 {
		t.Fatalf("second prune deleted %d", n)
	}
}
