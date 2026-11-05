package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A daily Jev limit stops calls before they reach Jev, quietly for the
// scheduler, and starts again the next day. 0 means no limit.
func TestJevDailyLimit(t *testing.T) {
	s, fj, id := jevSvc(t)
	ctx := context.Background()
	clock := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.SetJevDailyLimit(2)

	for i := range 2 {
		if _, err := s.JevRate(ctx, id); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if _, err := s.JevRate(ctx, id); !errors.Is(err, ErrJevLimit) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("third call = %v, want ErrJevLimit", err)
	}
	if fj.calls != 2 {
		t.Fatalf("Jev got %d calls, want 2", fj.calls)
	}

	// The scheduler isn't failing; it's done for today.
	if _, err := s.AddJob(ctx, NewJob{Title: "Second job", Company: "Duck Co", JD: "Go."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.runJevRate(ctx); err != nil {
		t.Fatalf("scheduled rating at the limit = %v, want a quiet stop", err)
	}
	if fj.calls != 2 {
		t.Fatalf("the scheduler called Jev past the limit: %d calls", fj.calls)
	}

	clock = clock.Add(24 * time.Hour)
	if _, err := s.JevRate(ctx, id); err != nil {
		t.Fatalf("next day: %v", err)
	}
	s.SetJevDailyLimit(0)
	for range 3 {
		if _, err := s.JevRate(ctx, id); err != nil {
			t.Fatalf("no limit: %v", err)
		}
	}
	if fj.calls != 6 {
		t.Fatalf("Jev got %d calls, want 6", fj.calls)
	}
}
