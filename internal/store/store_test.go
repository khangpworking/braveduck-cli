package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

var now = time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAddGetRoundTrip(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	in := jobs.Job{
		Title:             "Data Analyst",
		Company:           "MoMo",
		URL:               "https://vietnamworks.com/j/1",
		Salary:            "25 - 40 triệu",
		Skills:            "SQL,Python",
		Status:            "waiting",
		Score:             90,
		HasScore:          true,
		ScoreLetter:       "A",
		DiscoverySource:   "vietnamworks",
		ExpiresAt:         "expire:2026-08-15",
		CompanyDistanceKM: 5,
		HasDistance:       true,
		CVVersion:         2,
	}
	id, err := s.Add(ctx, in, now)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id || got.Title != in.Title || got.Company != in.Company ||
		got.Salary != in.Salary || got.Skills != in.Skills || got.Status != "waiting" ||
		got.ScoreLetter != "A" || got.DiscoverySource != "vietnamworks" ||
		got.ExpiresAt != in.ExpiresAt || got.CVVersion != 2 {
		t.Errorf("round trip mismatch: got %+v", got)
	}
	if !got.HasScore || got.Score != 90 {
		t.Errorf("score not preserved: has=%v val=%v", got.HasScore, got.Score)
	}
	if !got.HasDistance || got.CompanyDistanceKM != 5 {
		t.Errorf("distance not preserved: has=%v val=%v", got.HasDistance, got.CompanyDistanceKM)
	}
	// Timestamps backfilled from now.
	if got.SavedAt == "" || got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("timestamps not backfilled: %+v", got)
	}
}

func TestNullNumericsDistinctFromZero(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	// Job with no score / no distance set.
	id, err := s.Add(ctx, jobs.Job{Title: "X", Company: "Y"}, now)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, id)
	if got.HasScore {
		t.Error("unset score should read HasScore=false")
	}
	if got.HasDistance {
		t.Error("unset distance should read HasDistance=false")
	}
	if got.Status != "saved" {
		t.Errorf("default status = %q, want saved", got.Status)
	}
}

func TestListFilter(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.Add(ctx, jobs.Job{Title: "a", Status: "saved", DiscoverySource: "topcv"}, now)
	s.Add(ctx, jobs.Job{Title: "b", Status: "applied", DiscoverySource: "topcv"}, now.Add(time.Second))
	s.Add(ctx, jobs.Job{Title: "c", Status: "applied", DiscoverySource: "serpapi"}, now.Add(2*time.Second))

	all, _ := s.List(ctx, ListFilter{})
	if len(all) != 3 {
		t.Fatalf("List all = %d, want 3", len(all))
	}
	// newest first by saved_at
	if all[0].Title != "c" {
		t.Errorf("order: first = %q, want c", all[0].Title)
	}
	applied, _ := s.List(ctx, ListFilter{Status: "applied"})
	if len(applied) != 2 {
		t.Errorf("applied filter = %d, want 2", len(applied))
	}
	topcvApplied, _ := s.List(ctx, ListFilter{Status: "applied", Source: "topcv"})
	if len(topcvApplied) != 1 || topcvApplied[0].Title != "b" {
		t.Errorf("combined filter = %+v, want [b]", topcvApplied)
	}
}

func TestUpdateAndStateHelpers(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	id, _ := s.Add(ctx, jobs.Job{Title: "t", Company: "c", Status: "saved"}, now)

	later := now.Add(time.Hour)
	j, _ := s.Get(ctx, id)
	j.Notes = "call recruiter"
	j.Score, j.HasScore, j.ScoreLetter = 78, true, "B"
	if err := s.Update(ctx, j, later); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := s.Get(ctx, id)
	if got.Notes != "call recruiter" || got.ScoreLetter != "B" || !got.HasScore {
		t.Errorf("update not persisted: %+v", got)
	}
	if got.UpdatedAt != later.UTC().Format(time.RFC3339) {
		t.Errorf("updated_at not stamped: %q", got.UpdatedAt)
	}

	if err := s.SetStatus(ctx, id, "applied", later); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApplyState(ctx, id, "queued", "waiting for cooldown", later); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, id)
	if got.Status != "applied" || got.ApplyState != "queued" || got.ApplyDetail != "waiting for cooldown" {
		t.Errorf("state helpers: %+v", got)
	}
}

func TestDeleteAndMissing(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	id, _ := s.Add(ctx, jobs.Job{Title: "gone"}, now)
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get after delete = %v, want ErrNoRows", err)
	}
	if err := s.Delete(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Delete missing = %v, want ErrNoRows", err)
	}
	if err := s.SetStatus(ctx, 999, "applied", now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SetStatus missing = %v, want ErrNoRows", err)
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "bd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	s.Add(ctx, jobs.Job{Title: "kept"}, time.Now())

	dest := filepath.Join(dir, "copy.db")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file: %v %v, want private", fi, err)
	}
	c, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if list, _ := c.List(ctx, ListFilter{}); len(list) != 1 || list[0].Title != "kept" {
		t.Fatalf("backup holds %+v", list)
	}
	if err := s.Backup(ctx, dest); err == nil {
		t.Fatal("overwrote an existing file")
	}
}

// CloseIfUnchanged closes only while the job is as the caller read it.
func TestCloseIfUnchanged(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	id, _ := s.Add(ctx, jobs.Job{Title: "x", Status: "saved", Notes: "mine"}, now)
	j, _ := s.Get(ctx, id)
	if ok, err := s.CloseIfUnchanged(ctx, id, "saved", "2000-01-01T00:00:00Z", "swept", now); ok || err != nil {
		t.Fatalf("stale updated_at closed it: %v %v", ok, err)
	}
	if ok, err := s.CloseIfUnchanged(ctx, id, "applied", j.UpdatedAt, "swept", now); ok || err != nil {
		t.Fatalf("stale status closed it: %v %v", ok, err)
	}
	if ok, err := s.CloseIfUnchanged(ctx, id, "saved", j.UpdatedAt, "swept", now); !ok || err != nil {
		t.Fatalf("unchanged job not closed: %v %v", ok, err)
	}
	if got, _ := s.Get(ctx, id); got.Status != "closed" || got.Notes != "swept\nmine" {
		t.Fatalf("after = %q / %q", got.Status, got.Notes)
	}
}
