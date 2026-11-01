package store

import (
	"context"
	"database/sql"
	"time"
)

// Session statuses.
const (
	SessionConnected = "connected"
	SessionExpired   = "expired"
	SessionUnknown   = "unknown"
)

// PlatformSession is one platform's saved login. Enc is the vault-sealed
// session (cookies + storage); the store never sees plaintext.
type PlatformSession struct {
	Platform    string
	Enc         []byte
	CreatedAt   string
	ExpiresHint string
	FPSeed      string
	Status      string
	CheckedAt   string
	AccountHint string
}

// SaveSession inserts or replaces a platform's session.
func (s *Store) SaveSession(ctx context.Context, ps PlatformSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO platform_sessions
		   (platform, session_enc, created_at, expires_hint, fp_seed, status, checked_at, account_hint)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(platform) DO UPDATE SET
		   session_enc = excluded.session_enc, created_at = excluded.created_at,
		   expires_hint = excluded.expires_hint, fp_seed = excluded.fp_seed,
		   status = excluded.status, checked_at = excluded.checked_at,
		   account_hint = excluded.account_hint`,
		ps.Platform, ps.Enc, ps.CreatedAt, ps.ExpiresHint, ps.FPSeed, ps.Status, ps.CheckedAt, ps.AccountHint)
	return err
}

const sessionCols = `platform, session_enc, created_at, COALESCE(expires_hint,''), fp_seed,
  status, COALESCE(checked_at,''), COALESCE(account_hint,'')`

func scanSession(sc interface{ Scan(...any) error }) (PlatformSession, error) {
	var ps PlatformSession
	err := sc.Scan(&ps.Platform, &ps.Enc, &ps.CreatedAt, &ps.ExpiresHint, &ps.FPSeed,
		&ps.Status, &ps.CheckedAt, &ps.AccountHint)
	return ps, err
}

// GetSession returns a platform's session, or sql.ErrNoRows.
func (s *Store) GetSession(ctx context.Context, platform string) (PlatformSession, error) {
	return scanSession(s.db.QueryRowContext(ctx,
		"SELECT "+sessionCols+" FROM platform_sessions WHERE platform = ?", platform))
}

// ListSessions returns every saved session, by platform.
func (s *Store) ListSessions(ctx context.Context) ([]PlatformSession, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+sessionCols+" FROM platform_sessions ORDER BY platform")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformSession
	for rows.Next() {
		ps, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// SetSessionStatus records a probe result. Returns sql.ErrNoRows if the
// platform has no session.
func (s *Store) SetSessionStatus(ctx context.Context, platform, status string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE platform_sessions SET status = ?, checked_at = ? WHERE platform = ?",
		status, now.UTC().Format(time.RFC3339), platform)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

// DeleteSession removes a platform's session; deleted reports whether there
// was one.
func (s *Store) DeleteSession(ctx context.Context, platform string) (deleted bool, err error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM platform_sessions WHERE platform = ?", platform)
	if err != nil {
		return false, err
	}
	if err := affectedOne(res); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}
