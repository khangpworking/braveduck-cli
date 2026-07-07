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
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store is a handle to the local SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies the schema.
// Use ":memory:" for an ephemeral store. Foreign keys are enabled so the
// job_ratings cascade works.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
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

// Add inserts a new job and returns its id. now backfills any empty
// SavedAt/CreatedAt/UpdatedAt with a single consistent ISO timestamp.
func (s *Store) Add(ctx context.Context, j jobs.Job, now time.Time) (int64, error) {
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
	q := fmt.Sprintf("INSERT INTO jobs (%s) VALUES (%s)", strings.Join(writeCols, ","), ph)
	res, err := s.db.ExecContext(ctx, q, writeArgs(j)...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Get returns the job with the given id, or sql.ErrNoRows when absent.
func (s *Store) Get(ctx context.Context, id int64) (jobs.Job, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+selectCols+" FROM jobs WHERE id = ?", id)
	return scanJob(row)
}

// ListFilter narrows List; empty fields mean "no filter on that column".
type ListFilter struct {
	Status string
	Source string
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
	return out, rows.Err()
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
