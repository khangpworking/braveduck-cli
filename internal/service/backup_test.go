package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/store"
)

func TestDailyBackupsKeepAWeek(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "braveduck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	s.SetDataDir(dir)
	clock := time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	s.now = func() time.Time { return clock }
	ctx := context.Background()

	s.backupIfDue(ctx)
	s.backupIfDue(ctx) // same day: not again
	if b, _ := s.backups(); len(b) != 1 {
		t.Fatalf("%d backups after two checks on one day, want 1", len(b))
	}
	for day := 1; day <= 9; day++ {
		clock = clock.Add(backupEvery)
		s.backupIfDue(ctx)
	}
	b, _ := s.backups()
	if len(b) != backupKeep || filepath.Base(b[len(b)-1]) != "braveduck-20261114-090000.db" {
		t.Fatalf("backups = %v, want the newest %d", b, backupKeep)
	}
	if fi, err := os.Stat(filepath.Join(dir, "backups")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("backups dir: %v %v, want 0700", fi, err)
	}
}

// Manual backups have their own name: they're never pruned and never push
// the daily copies out. A failed copy leaves no file that looks like one.
func TestManualBackupsAndFailures(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "braveduck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	s.SetDataDir(dir)
	clock := time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	s.now = func() time.Time { return clock }
	ctx := context.Background()

	s.backupIfDue(ctx)
	for i := 0; i < backupKeep+2; i++ {
		clock = clock.Add(time.Minute)
		if _, err := s.Backup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	daily, _ := s.backups()
	manual, _ := filepath.Glob(filepath.Join(dir, "backups", "braveduck-manual-*.db"))
	if len(daily) != 1 || len(manual) != backupKeep+2 {
		t.Fatalf("daily %v, manual %d: manual backups must not prune or count as daily", daily, len(manual))
	}

	// A copy that can't be written (here its temp name is taken by a
	// directory) fails without leaving a backup behind.
	clock = clock.Add(backupEvery)
	next := filepath.Join(dir, "backups", "braveduck-"+clock.Format("20060102-150405")+".db")
	os.Mkdir(next+".partial", 0o700)
	if err := s.dailyBackup(ctx); err == nil {
		t.Fatal("a failed copy reported success")
	}
	if _, err := os.Stat(next); !os.IsNotExist(err) {
		t.Fatalf("a failed copy left %s", next)
	}
}
