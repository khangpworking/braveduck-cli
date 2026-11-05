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

// Browsers under one owner take turns; other owners aren't held up.
func TestBrowserOwnerTurns(t *testing.T) {
	SetMaxBrowsers(2)
	t.Cleanup(func() { SetMaxBrowsers(0) })
	alice, bob := WithOwner(context.Background(), "alice"), WithOwner(context.Background(), "bob")

	ra, err := acquire(alice)
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(alice, 50*time.Millisecond)
	defer cancel()
	if _, err := acquire(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("alice's second browser at once: %v", err)
	}
	rb, err := acquire(bob)
	if err != nil {
		t.Fatalf("bob held up by alice: %v", err)
	}
	ra()
	ra2, err := acquire(alice)
	if err != nil {
		t.Fatalf("alice after her first browser ended: %v", err)
	}
	ra2()
	rb()

	SetMaxBrowsers(0) // no global cap: still one per owner
	r1, _ := acquire(alice)
	short2, cancel2 := context.WithTimeout(alice, 50*time.Millisecond)
	defer cancel2()
	if _, err := acquire(short2); err == nil {
		t.Fatal("two browsers for one owner without a global cap")
	}
	r1()
}
