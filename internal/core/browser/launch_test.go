package browser

import (
	"context"
	"errors"
	"testing"
	"time"
)

// SetMaxBrowsers makes launches wait for a free slot; giving a slot back
// twice frees it only once.
func TestBrowserSlots(t *testing.T) {
	SetMaxBrowsers(1)
	t.Cleanup(func() { SetMaxBrowsers(0) })

	release, err := acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second browser while the only slot is taken: %v", err)
	}
	release()
	release()
	r2, err := acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if _, err := acquire(ctx2); err == nil {
		t.Fatal("a double release freed two slots")
	}
	r2()

	SetMaxBrowsers(0)
	for range 3 {
		if _, err := acquire(context.Background()); err != nil {
			t.Fatalf("no cap: %v", err)
		}
	}
}
