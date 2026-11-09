package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestMigration13ClearsAllCompanyAnswersOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v12.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, migration := range migrations[:12] {
		if _, err := db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	for i, date := range []string{"2009-10-20", "not_found", "no_date", ""} {
		if _, err := db.Exec(`INSERT INTO jobs (title, company, status, saved_at, created_at, updated_at,
			company_active_date, company_masothue_url, company_masothue_name, company_tax_code, company_reg_status)
			VALUES ('keep title', 'keep company', 'closed', '2026-01-01', '2026-01-01', '2026-01-01', ?, 'url', 'name', 'tax', 'status')`, date); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO company_cache (name_key, found, at) VALUES (?, ?, '2026-01-01')`, fmt.Sprint(i), i%2); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 12"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	var cached, stale, kept int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM company_cache").Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE company_active_date IS NOT NULL OR company_masothue_url IS NOT NULL
		OR company_masothue_name IS NOT NULL OR company_tax_code IS NOT NULL OR company_reg_status IS NOT NULL`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM jobs WHERE title = 'keep title' AND company = 'keep company' AND status = 'closed'").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if cached != 0 || stale != 0 || kept != 4 {
		t.Fatalf("cache=%d, stale=%d, preserved jobs=%d", cached, stale, kept)
	}
	ctx := context.Background()
	if err := s.PutCompany(ctx, "fresh", CompanyInfo{Found: true, Founded: "2021-01-01"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.SetJobCompany(ctx, 1, "keep company", "2021-01-01", CompanyInfo{TaxCode: "0101778163"}); err != nil || !changed {
		t.Fatalf("new answer: %v, %v", changed, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetCompany(ctx, "fresh"); err != nil || !ok {
		t.Fatalf("reopen erased fresh cache: %v, %v", ok, err)
	}
	var date string
	if err := s.db.QueryRow("SELECT company_active_date FROM jobs WHERE id = 1").Scan(&date); err != nil || date != "2021-01-01" {
		t.Fatalf("reopen erased new answer: %s, %v", date, err)
	}
}
