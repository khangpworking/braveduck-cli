// Package store is the SQLite persistence layer. It owns the schema and the
// job CRUD used by the CLI, REST API and MCP layers. It uses the pure-Go
// modernc.org/sqlite driver (no cgo) so cross-compilation stays trivial.
//
// Every write takes the current time explicitly (like the core logic) so tests
// stay deterministic. Reads project NULL columns to zero values via COALESCE;
// the three genuinely-nullable numeric columns (score, distance, cv_version) are
// scanned through Null wrappers so "unset" is distinguishable from zero.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store is a handle to the local SQLite database.
type Store struct {
	db     *sql.DB
	memory bool // :memory: — single connection, nothing else can write
}

// migrations are applied in order, each exactly once, tracked by SQLite's
// PRAGMA user_version (version N means migrations[:N] have run). Append new
// steps; never edit or reorder a shipped one. Step 1 is the v1 baseline — it is
// idempotent (IF NOT EXISTS), so databases created before versioning existed
// adopt it cleanly.
var migrations = []string{
	schemaSQL,
	// 2: duplicate-URL checks (AddJob) scan by url. Deliberately not UNIQUE —
	// a DB that already holds duplicates would then fail to open at all.
	`CREATE INDEX IF NOT EXISTS idx_jobs_url ON jobs(url)`,
	// 3: platform sessions (docs/specs/platform-sessions.md §4).
	`ALTER TABLE platform_sessions ADD COLUMN fp_seed TEXT NOT NULL DEFAULT '';
	 ALTER TABLE platform_sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'unknown';
	 ALTER TABLE platform_sessions ADD COLUMN checked_at TEXT;
	 ALTER TABLE platform_sessions ADD COLUMN account_hint TEXT;`,
	// 4: external crawlers report each run (braveduck crawl-report).
	`ALTER TABLE crawl_runs ADD COLUMN last_success_at TEXT;
	 ALTER TABLE crawl_runs ADD COLUMN last_ok INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE crawl_runs ADD COLUMN last_error TEXT;`,
	// 5: how often each source fetches new jobs (0 = off), for every source.
	`CREATE TABLE IF NOT EXISTS crawl_schedule (
	   source        TEXT PRIMARY KEY,
	   every_minutes INTEGER NOT NULL
	 );`,
	// 6: assisted-apply attempts, for the daily cap and the cooldown.
	`CREATE TABLE IF NOT EXISTS apply_attempts (at TEXT NOT NULL);
	 CREATE INDEX IF NOT EXISTS idx_apply_attempts_at ON apply_attempts(at);`,
	// 7: the user's location (one row) and a geocoding cache (each address
	// is looked up once, per the OpenStreetMap usage policy).
	`CREATE TABLE IF NOT EXISTS user_location (
	   id     INTEGER PRIMARY KEY CHECK (id = 1),
	   lat    REAL NOT NULL,
	   lng    REAL NOT NULL,
	   label  TEXT NOT NULL DEFAULT '',
	   max_km REAL NOT NULL DEFAULT 10
	 );
	 CREATE TABLE IF NOT EXISTS geocode_cache (
	   query TEXT PRIMARY KEY,
	   found INTEGER NOT NULL,
	   lat   REAL,
	   lng   REAL,
	   label TEXT,
	   at    TEXT NOT NULL
	 );`,
	// 8: small key/value settings (e.g. which sources get the Jev lead check).
	`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
	// 9: Facebook groups the user follows (pasted group links); a scanner
	// reports each group's name and whether it's public.
	`CREATE TABLE IF NOT EXISTS fb_groups (
	   group_key  TEXT PRIMARY KEY,
	   url        TEXT NOT NULL,
	   name       TEXT NOT NULL DEFAULT '',
	   visibility TEXT NOT NULL DEFAULT 'checking',
	   note       TEXT NOT NULL DEFAULT '',
	   added_at   TEXT NOT NULL,
	   checked_at TEXT NOT NULL DEFAULT ''
	 );`,
}

// Open opens (creating if needed) the database at path and migrates it to the
// current schema. Use ":memory:" for an ephemeral store.
//
// Pragmas go in the DSN so they apply to every pooled connection, not just the
// first: foreign keys (for the job_ratings cascade), a busy timeout so the CLI
// and a running `serve` don't fail with "database is locked", and WAL so readers
// never block the writer.
func Open(path string) (*Store, error) {
	pragmas := "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	memory := path == ":memory:"
	if !memory {
		pragmas += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", path+"?"+pragmas)
	if err != nil {
		return nil, err
	}
	if memory {
		// Each :memory: connection is its own empty database; pin to one.
		db.SetMaxOpenConns(1)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, memory: memory}, nil
}

// migrate brings db up to len(migrations), one transaction per step.
func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("database schema v%d is newer than this braveduck (v%d) — upgrade braveduck", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		// PRAGMA can't take a bound parameter; i+1 is our own int.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion reports the database's applied migration count (for doctor).
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// selectCols is the column projection shared by Get and List; ordering must match
// scanJob. NULL text columns collapse to "" via COALESCE; the numeric nullables
// (score, company_distance_km, cv_version) pass through raw for Null scanning.
const selectCols = `
  id, title, COALESCE(company,''), COALESCE(url,''), COALESCE(location,''),
  status, pinned, keep, COALESCE(notes,''),
  COALESCE(salary,''), COALESCE(skills,''), COALESCE(jd_summary,''),
  COALESCE(jd_ai_summary,''), COALESCE(jd_full,''), COALESCE(discovery_source,''),
  score, COALESCE(score_letter,''), COALESCE(score_status,''), COALESCE(score_reasoning,''),
  COALESCE(apply_method,''), COALESCE(apply_state,''), COALESCE(apply_next_at,''),
  COALESCE(apply_detail,''), COALESCE(apply_updated_at,''),
  COALESCE(cv_path,''), COALESCE(cv_status,''), cv_version, COALESCE(cv_generated_at,''),
  COALESCE(draft_email,''), COALESCE(draft_status,''), COALESCE(draft_status_at,''),
  COALESCE(draft_notes,''), COALESCE(contact_email,''),
  COALESCE(expires_at,''), COALESCE(followup_sent_at,''), saved_at, created_at, updated_at,
  COALESCE(topcv_status,''), COALESCE(vnw_status,''),
  COALESCE(company_active_date,''), company_distance_km, COALESCE(company_geo_status,''),
  COALESCE(company_masothue_url,''), COALESCE(company_masothue_name,'')`

func scanJob(sc interface{ Scan(...any) error }) (jobs.Job, error) {
	var j jobs.Job
	var pinned, keep int
	var score, dist sql.NullFloat64
	var cvVer sql.NullInt64
	err := sc.Scan(
		&j.ID, &j.Title, &j.Company, &j.URL, &j.Location,
		&j.Status, &pinned, &keep, &j.Notes,
		&j.Salary, &j.Skills, &j.Summary,
		&j.AISummary, &j.FullJD, &j.DiscoverySource,
		&score, &j.ScoreLetter, &j.ScoreStatus, &j.ScoreReasoning,
		&j.ApplyMethod, &j.ApplyState, &j.ApplyNextAt,
		&j.ApplyDetail, &j.ApplyUpdatedAt,
		&j.CVPath, &j.CVStatus, &cvVer, &j.CVGeneratedAt,
		&j.DraftEmail, &j.DraftStatus, &j.DraftStatusAt,
		&j.DraftNotes, &j.ContactEmail,
		&j.ExpiresAt, &j.FollowupSentAt, &j.SavedAt, &j.CreatedAt, &j.UpdatedAt,
		&j.TopcvStatus, &j.VnwStatus,
		&j.CompanyActiveDate, &dist, &j.CompanyGeoStatus,
		&j.CompanyMasothueURL, &j.CompanyMasothueName,
	)
	if err != nil {
		return jobs.Job{}, err
	}
	j.Pinned, j.Keep = pinned != 0, keep != 0
	j.Score, j.HasScore = score.Float64, score.Valid
	j.CompanyDistanceKM, j.HasDistance = dist.Float64, dist.Valid
	if cvVer.Valid {
		j.CVVersion = int(cvVer.Int64)
	}
	return j, nil
}

// writeCols are the insertable/updatable columns (everything except id), in the
// order writeArgs supplies them.
var writeCols = []string{
	"title", "company", "url", "location",
	"status", "pinned", "keep", "notes",
	"salary", "skills", "jd_summary", "jd_ai_summary", "jd_full", "discovery_source",
	"score", "score_letter", "score_status", "score_reasoning",
	"apply_method", "apply_state", "apply_next_at", "apply_detail", "apply_updated_at",
	"cv_path", "cv_status", "cv_version", "cv_generated_at",
	"draft_email", "draft_status", "draft_status_at", "draft_notes", "contact_email",
	"expires_at", "followup_sent_at", "saved_at", "created_at", "updated_at",
	"topcv_status", "vnw_status",
	"company_active_date", "company_distance_km", "company_geo_status",
	"company_masothue_url", "company_masothue_name",
}

func writeArgs(j jobs.Job) []any {
	nf := func(v float64, ok bool) any {
		if !ok {
			return nil
		}
		return v
	}
	return []any{
		j.Title, j.Company, j.URL, j.Location,
		j.Status, b2i(j.Pinned), b2i(j.Keep), j.Notes,
		j.Salary, j.Skills, j.Summary, j.AISummary, j.FullJD, j.DiscoverySource,
		nf(j.Score, j.HasScore), j.ScoreLetter, j.ScoreStatus, j.ScoreReasoning,
		j.ApplyMethod, j.ApplyState, j.ApplyNextAt, j.ApplyDetail, j.ApplyUpdatedAt,
		j.CVPath, j.CVStatus, j.CVVersion, j.CVGeneratedAt,
		j.DraftEmail, j.DraftStatus, j.DraftStatusAt, j.DraftNotes, j.ContactEmail,
		j.ExpiresAt, j.FollowupSentAt, j.SavedAt, j.CreatedAt, j.UpdatedAt,
		j.TopcvStatus, j.VnwStatus,
		j.CompanyActiveDate, nf(j.CompanyDistanceKM, j.HasDistance), j.CompanyGeoStatus,
		j.CompanyMasothueURL, j.CompanyMasothueName,
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ErrDuplicateURL is returned by Add when a job with the same non-empty URL is
// already stored.
var ErrDuplicateURL = errors.New("this job is already on the board")

// ErrJobLimit means an insert would exceed the board's configured job cap.
var ErrJobLimit = errors.New("the board is full")

// Add inserts a new job and returns its id. now backfills any empty
// SavedAt/CreatedAt/UpdatedAt with a single consistent ISO timestamp.
//
// A non-empty URL must be unique: the existence check and the insert are one
// statement, so two concurrent adds of the same link can't both succeed.
func (s *Store) Add(ctx context.Context, j jobs.Job, now time.Time) (int64, error) {
	return s.AddWithLimit(ctx, j, now, 0)
}

// AddWithLimit inserts only when the board has room. The capacity check and
// insert are one write statement, including across separate database handles.
// A non-positive limit leaves the board uncapped.
func (s *Store) AddWithLimit(ctx context.Context, j jobs.Job, now time.Time, limit int) (int64, error) {
	ts := now.UTC().Format(time.RFC3339)
	if j.SavedAt == "" {
		j.SavedAt = ts
	}
	if j.CreatedAt == "" {
		j.CreatedAt = ts
	}
	j.UpdatedAt = ts
	if j.Status == "" {
		j.Status = "saved"
	}

	ph := strings.TrimSuffix(strings.Repeat("?,", len(writeCols)), ",")
	q := fmt.Sprintf(
		`INSERT INTO jobs (%s) SELECT %s
		 WHERE (? = '' OR NOT EXISTS (SELECT 1 FROM jobs WHERE url = ?))
		 AND (? <= 0 OR (SELECT COUNT(*) FROM jobs) < ?)`,
		strings.Join(writeCols, ","), ph)
	// Keep the write lock until a rejected insert is classified, so a racing
	// deletion cannot turn a full-board error into a duplicate error.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, q, append(writeArgs(j), j.URL, j.URL, limit, limit)...)
	if err != nil {
		return 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return 0, err
	} else if n == 0 {
		if limit > 0 {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs").Scan(&count); err != nil {
				return 0, err
			}
			if count >= limit {
				return 0, ErrJobLimit
			}
		}
		return 0, ErrDuplicateURL
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Get returns the job with the given id, or sql.ErrNoRows when absent.
func (s *Store) Get(ctx context.Context, id int64) (jobs.Job, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+selectCols+" FROM jobs WHERE id = ?", id)
	j, err := scanJob(row)
	if err != nil {
		return j, err
	}
	list := []jobs.Job{j}
	err = s.loadRatings(ctx, list)
	return list[0], err
}

// ListFilter narrows List; empty fields mean "no filter on that column".
type ListFilter struct {
	Status string
	Source string
	Query  string // words to search for; applied by the service (accent-insensitive), not here
}

// List returns jobs matching the filter, newest first by saved_at.
func (s *Store) List(ctx context.Context, f ListFilter) ([]jobs.Job, error) {
	q := "SELECT " + selectCols + " FROM jobs"
	var where []string
	var args []any
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.Source != "" {
		where = append(where, "discovery_source = ?")
		args = append(args, f.Source)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY saved_at DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []jobs.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := s.loadRatings(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update overwrites every column of an existing job (by j.ID) and stamps
// updated_at with now. Returns sql.ErrNoRows if the id doesn't exist.
func (s *Store) Update(ctx context.Context, j jobs.Job, now time.Time) error {
	j.UpdatedAt = now.UTC().Format(time.RFC3339)
	set := make([]string, len(writeCols))
	for i, c := range writeCols {
		set[i] = c + " = ?"
	}
	q := fmt.Sprintf("UPDATE jobs SET %s WHERE id = ?", strings.Join(set, ","))
	args := append(writeArgs(j), j.ID)
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// JobPatch names the board-editable fields to change; nil means "leave as is".
// Patch writes only the named columns, so two clients editing different fields
// of the same job can't clobber each other (a full-row Update could).
type JobPatch struct {
	Status    *string
	Pinned    *bool
	Keep      *bool
	Notes     *string
	Title     *string
	Company   *string
	Salary    *string
	Location  *string
	ExpiresAt *string // the expires_at grammar (jobs.ParseExpiry); set by the service, not the board
}

// Patch applies p to job id, stamping updated_at. An empty patch still checks
// the job exists. Returns sql.ErrNoRows if it doesn't.
func (s *Store) Patch(ctx context.Context, id int64, p JobPatch, now time.Time) error {
	set := []string{"updated_at = ?"}
	args := []any{now.UTC().Format(time.RFC3339)}
	if p.Status != nil {
		set, args = append(set, "status = ?"), append(args, *p.Status)
	}
	if p.Pinned != nil {
		set, args = append(set, "pinned = ?"), append(args, b2i(*p.Pinned))
	}
	if p.Keep != nil {
		set, args = append(set, "keep = ?"), append(args, b2i(*p.Keep))
	}
	for col, v := range map[string]*string{
		"notes": p.Notes, "title": p.Title, "company": p.Company, "salary": p.Salary, "location": p.Location,
		"expires_at": p.ExpiresAt,
	} {
		if v != nil {
			set, args = append(set, col+" = ?"), append(args, *v)
		}
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET "+strings.Join(set, ", ")+" WHERE id = ?", append(args, id)...)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// Toggle flips a boolean flag ("pinned" or "keep") in one statement, so two
// rapid clicks can't read the same old value and both write the same new one.
func (s *Store) Toggle(ctx context.Context, id int64, flag string, now time.Time) error {
	if flag != "pinned" && flag != "keep" {
		return fmt.Errorf("store: can't toggle %q", flag)
	}
	res, err := s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE jobs SET %[1]s = 1 - %[1]s, updated_at = ? WHERE id = ?", flag),
		now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// SetStatus moves a job to a new lane, stamping updated_at.
func (s *Store) SetStatus(ctx context.Context, id int64, status string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?",
		status, now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// SetApplyState updates the apply pipeline state (and optional detail), stamping
// both apply_updated_at and updated_at.
func (s *Store) SetApplyState(ctx context.Context, id int64, state, detail string, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET apply_state = ?, apply_detail = ?, apply_updated_at = ?, updated_at = ? WHERE id = ?",
		state, detail, ts, ts, id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// Delete removes a job (and its ratings, via cascade).
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM jobs WHERE id = ?", id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// IsNotFound reports whether err is the store's "no such row" error, so callers
// (the API/MCP faces) can map it without importing database/sql.
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// affectedOne turns a zero-row write into sql.ErrNoRows so callers can tell
// "no such id" apart from success.
func affectedOne(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetTopcvStatus records a job's TopCV application status (a short code) and
// the site's own status line, stamping updated_at.
func (s *Store) SetTopcvStatus(ctx context.Context, id int64, status, detail string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET topcv_status = ?, apply_detail = ?, updated_at = ? WHERE id = ?",
		status, detail, now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// SetCV records a job's current tailored CV file and version.
func (s *Store) SetCV(ctx context.Context, id int64, path string, version int, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET cv_path = ?, cv_version = ?, cv_status = 'ready', cv_generated_at = ?, updated_at = ? WHERE id = ?",
		path, version, ts, ts, id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// SetDraft stores a job's email draft (JSON, or "" to clear it) and status.
func (s *Store) SetDraft(ctx context.Context, id int64, draftJSON, status string, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET draft_email = ?, draft_status = ?, draft_status_at = ?, updated_at = ? WHERE id = ?",
		draftJSON, status, ts, ts, id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// SetDraftNotes stores the user's steering notes for the next draft.
func (s *Store) SetDraftNotes(ctx context.Context, id int64, notes string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET draft_notes = ?, updated_at = ? WHERE id = ?",
		notes, now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// RecordApplyAttempt logs one assisted-apply window.
func (s *Store) RecordApplyAttempt(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO apply_attempts (at) VALUES (?)", now.UTC().Format(time.RFC3339))
	return err
}

// ApplyAttempts returns attempt times since since, oldest first.
func (s *Store) ApplyAttempts(ctx context.Context, since time.Time) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT at FROM apply_attempts WHERE at >= ? ORDER BY at", since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// CountJobs is how many jobs are on the board, in every lane.
func (s *Store) CountJobs(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs").Scan(&n)
	return n, err
}

// Setting returns a stored setting; ok is false if unset.
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// Backup writes a consistent copy of the database to dest (which must not
// exist yet) with VACUUM INTO, safe while the board is running, and makes
// it readable by the user only.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup: %s already exists", dest)
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return os.Chmod(dest, 0o600)
}

// CloseIfUnchanged moves job id to Closed with note prepended to its notes,
// only while its status and updated_at are still the ones the caller read —
// a concurrent edit or move wins. It reports whether the job was closed.
func (s *Store) CloseIfUnchanged(ctx context.Context, id int64, status, updatedAt, note string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'closed',
		notes = CASE WHEN COALESCE(notes, '') = '' THEN ? ELSE ? || char(10) || notes END,
		updated_at = ?
		WHERE id = ? AND status = ? AND COALESCE(updated_at, '') = ?`,
		note, note, now.UTC().Format(time.RFC3339), id, status, updatedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
