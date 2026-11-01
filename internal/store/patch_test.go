package store

import (
	"context"
	"errors"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

func ptr[T any](v T) *T { return &v }

func TestPatchTouchesOnlyNamedFields(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	id, err := s.Add(ctx, jobs.Job{Title: "X", Notes: "keep me", Salary: "20M"}, now)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a concurrent full-row writer having changed salary in between: a
	// field patch must not revert it.
	if _, err := s.db.Exec("UPDATE jobs SET salary = '30M' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	if err := s.Patch(ctx, id, JobPatch{Status: ptr("applied"), Pinned: ptr(true)}, now); err != nil {
		t.Fatal(err)
	}
	j, _ := s.Get(ctx, id)
	if j.Status != "applied" || !j.Pinned || j.Keep || j.Notes != "keep me" || j.Salary != "30M" {
		t.Fatalf("patch clobbered or missed fields: %+v", j)
	}

	if err := s.Patch(ctx, 999, JobPatch{Keep: ptr(true)}, now); !IsNotFound(err) {
		t.Fatalf("patch missing job = %v, want not-found", err)
	}
}

func TestToggle(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	id, _ := s.Add(ctx, jobs.Job{Title: "X"}, now)

	for i, want := range []bool{true, false, true} {
		if err := s.Toggle(ctx, id, "pinned", now); err != nil {
			t.Fatal(err)
		}
		if j, _ := s.Get(ctx, id); j.Pinned != want {
			t.Fatalf("toggle %d: pinned=%v want %v", i, j.Pinned, want)
		}
	}
	if err := s.Toggle(ctx, id, "status", now); err == nil {
		t.Fatal("toggling a non-boolean column was allowed")
	}
	if err := s.Toggle(ctx, 999, "keep", now); !IsNotFound(err) {
		t.Fatalf("toggle missing job = %v, want not-found", err)
	}
}

func TestAddRejectsDuplicateURL(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if _, err := s.Add(ctx, jobs.Job{Title: "A", URL: "https://topcv.vn/j/1"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, jobs.Job{Title: "B", URL: "https://topcv.vn/j/1"}, now); !errors.Is(err, ErrDuplicateURL) {
		t.Fatalf("duplicate add = %v, want ErrDuplicateURL", err)
	}
	// Empty URLs (pasted JDs) never collide.
	for i := 0; i < 2; i++ {
		if _, err := s.Add(ctx, jobs.Job{Title: "pasted"}, now); err != nil {
			t.Fatalf("empty-URL add %d: %v", i, err)
		}
	}
	if list, _ := s.List(ctx, ListFilter{}); len(list) != 3 {
		t.Fatalf("got %d jobs, want 3", len(list))
	}
}
