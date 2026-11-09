package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CompanyInfo is a cached business-register lookup; Found false means the
// register has no single company with the name (URL is then its search).
type CompanyInfo struct {
	Found   bool
	TaxCode string
	Name    string
	Founded string // YYYY-MM-DD, or "" if the register gives no date
	Status  string
	URL     string
}

// GetCompany returns the cached lookup for key; ok is false if never looked up.
func (s *Store) GetCompany(ctx context.Context, key string) (CompanyInfo, bool, error) {
	var c CompanyInfo
	var found int
	err := s.db.QueryRowContext(ctx, "SELECT found, tax_code, name, founded, status, url FROM company_cache WHERE name_key = ?", key).
		Scan(&found, &c.TaxCode, &c.Name, &c.Founded, &c.Status, &c.URL)
	if errors.Is(err, sql.ErrNoRows) {
		return CompanyInfo{}, false, nil
	}
	if err != nil {
		return CompanyInfo{}, false, err
	}
	c.Found = found == 1
	return c, true, nil
}

// PutCompany caches a lookup (including "not found", so it isn't repeated).
func (s *Store) PutCompany(ctx context.Context, key string, c CompanyInfo, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO company_cache (name_key, found, tax_code, name, founded, status, url, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name_key) DO UPDATE SET found = excluded.found, tax_code = excluded.tax_code, name = excluded.name,
		   founded = excluded.founded, status = excluded.status, url = excluded.url, at = excluded.at`,
		key, b2i(c.Found), c.TaxCode, c.Name, c.Founded, c.Status, c.URL, now.UTC().Format(time.RFC3339))
	return err
}

// SetJobCompany records on a job what the register said; activeDate is the
// founding day or one of the jobs.Company* sentinels. A stale lookup cannot
// overwrite an edited name or an answer written while it was in flight.
func (s *Store) SetJobCompany(ctx context.Context, id int64, name, activeDate string, c CompanyInfo) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET company_active_date = ?, company_masothue_url = ?, company_masothue_name = ?,
		   company_tax_code = ?, company_reg_status = ?
		 WHERE id = ? AND company IS ? AND COALESCE(company_active_date, '') = ''`,
		activeDate, c.URL, c.Name, c.TaxCode, c.Status, id, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
