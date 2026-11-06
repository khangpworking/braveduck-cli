package store

import (
	"context"
	"time"
)

// FBGroup is a Facebook group the user follows.
type FBGroup struct {
	Key        string `json:"key"` // the id or name in facebook.com/groups/<key>
	URL        string `json:"url"`
	Name       string `json:"name"`       // "" until the scanner has seen it
	Visibility string `json:"visibility"` // checking / public / private / unavailable
	Note       string `json:"note"`       // why it can't be read, if it can't
	AddedAt    string `json:"added_at"`
	CheckedAt  string `json:"checked_at"`
}

// AddFBGroup follows a group; added is false if it was followed already.
func (s *Store) AddFBGroup(ctx context.Context, key, url string, now time.Time) (added bool, err error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO fb_groups (group_key, url, added_at) VALUES (?, ?, ?)",
		key, url, now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// FBGroups lists the followed groups, oldest first.
func (s *Store) FBGroups(ctx context.Context) ([]FBGroup, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT group_key, url, name, visibility, note, added_at, checked_at FROM fb_groups ORDER BY added_at, group_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FBGroup{}
	for rows.Next() {
		var g FBGroup
		if err := rows.Scan(&g.Key, &g.URL, &g.Name, &g.Visibility, &g.Note, &g.AddedAt, &g.CheckedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// FBGroup returns one followed group; ok is false if it isn't followed.
func (s *Store) FBGroup(ctx context.Context, key string) (g FBGroup, ok bool, err error) {
	all, err := s.FBGroups(ctx)
	for _, x := range all {
		if x.Key == key {
			return x, true, err
		}
	}
	return FBGroup{}, false, err
}

// DeleteFBGroup unfollows a group; deleted is false if it wasn't followed.
func (s *Store) DeleteFBGroup(ctx context.Context, key string) (deleted bool, err error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM fb_groups WHERE group_key = ?", key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SetFBGroupStatus records what the scanner found about a group. An empty
// name keeps the one already known.
func (s *Store) SetFBGroupStatus(ctx context.Context, key, name, visibility, note string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE fb_groups SET name = CASE WHEN ? = '' THEN name ELSE ? END,
		   visibility = ?, note = ?, checked_at = ? WHERE group_key = ?`,
		name, name, visibility, note, now.UTC().Format(time.RFC3339), key)
	return err
}
