package store

import (
	"context"
	"time"
)

// Event is one line of the board's activity feed. Timestamp is the ISO-8601 UTC
// instant the event was recorded; the JSON keys match what the web board renders.
type Event struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// LogEvent appends an activity-feed entry. Callers treat this as best-effort —
// a failed log must never fail the underlying job mutation.
func (s *Store) LogEvent(ctx context.Context, typ, message string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO events (type, message, created_at) VALUES (?, ?, ?)",
		typ, message, now.UTC().Format(time.RFC3339))
	return err
}

// RecentEvents returns the newest events first, capped at limit (defaulted to 20
// when non-positive).
func (s *Store) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT type, message, created_at FROM events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Type, &e.Message, &e.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneEvents keeps the newest keep events and deletes the rest, so the
// activity feed can't grow without bound.
func (s *Store) PruneEvents(ctx context.Context, keep int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM events WHERE id <= (SELECT id FROM events ORDER BY id DESC LIMIT 1 OFFSET ?)", keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
