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
