package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backups: the board is the user's only copy of their job hunt, so while it
// runs braveduck copies the database once a day into <data dir>/backups and
// keeps the last backupKeep copies. `braveduck backup` makes one on demand.
// To restore, stop the board and copy a backup over braveduck.db.

const (
	backupKeep  = 7
	backupEvery = 24 * time.Hour
)

// Backup copies the database into the backups folder and prunes old copies;
// it returns the new file's path.
func (s *Service) Backup(ctx context.Context) (string, error) {
	dir, err := s.backupDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "braveduck-"+s.now().Format("20060102-150405")+".db")
	if err := s.st.Backup(ctx, path); err != nil {
		return "", err
	}
	old, _ := s.backups()
	for len(old) > backupKeep {
		_ = os.Remove(old[0])
		old = old[1:]
	}
	return path, nil
}

// BackupTo copies the database to path (which must not exist yet).
func (s *Service) BackupTo(ctx context.Context, path string) error {
	return s.st.Backup(ctx, path)
}

// backups lists the backup files, oldest first (their names sort by time).
func (s *Service) backups() ([]string, error) {
	dir, err := s.backupDir()
	if err != nil {
		return nil, err
	}
	names, err := filepath.Glob(filepath.Join(dir, "braveduck-*.db"))
	sort.Strings(names)
	return names, err
}

func (s *Service) backupDir() (string, error) {
	if s.profilePath == "" {
		return "", errors.New("no data dir configured")
	}
	return filepath.Join(filepath.Dir(s.profilePath), "backups"), nil
}

// backupIfDue makes the daily backup once backupEvery has passed since the
// newest one (on disk, so a restarted board doesn't back up again at once).
func (s *Service) backupIfDue(ctx context.Context) {
	names, err := s.backups()
	if err != nil {
		return
	}
	if n := len(names); n > 0 {
		stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(names[n-1]), "braveduck-"), ".db")
		if t, err := time.ParseInLocation("20060102-150405", stamp, s.now().Location()); err == nil && s.now().Sub(t) < backupEvery {
			return
		}
	}
	_, _ = s.Backup(ctx)
}
