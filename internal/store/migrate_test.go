package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateStampsVersionAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bd.db")
	ctx := context.Background()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != len(migrations) {
		t.Fatalf("version = %d, want %d", v, len(migrations))
	}
	s.Close()

	// Reopening an up-to-date DB is a no-op, not a re-run.
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bd.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open on future schema = %v, want a 'newer' error", err)
	}
}

// Foreign keys must hold on every pooled connection, not just the first one
// opened — otherwise the job_ratings cascade silently stops working.
func TestForeignKeysOnEveryConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		c, err := s.db.Conn(ctx) // force distinct connections
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		var fk int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatal(err)
		}
		if fk != 1 {
			t.Fatalf("conn %d: foreign_keys = %d, want 1", i, fk)
		}
	}
}
