package store

import (
	"context"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
)

// ListKeywords returns the stored terms for one keyword list, as the user
// entered them (case/accents preserved), ordered alphabetically for stable
// display. An unknown list key returns an error so typos surface early.
func (s *Store) ListKeywords(ctx context.Context, listKey string) ([]string, error) {
	if _, ok := keyword.ListByKey(listKey); !ok {
		return nil, unknownList(listKey)
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT term FROM keyword_terms WHERE list_key = ? ORDER BY term", listKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AllKeywords loads every managed list into the keyword.Sets shape Filter wants.
func (s *Store) AllKeywords(ctx context.Context) (keyword.Sets, error) {
	sets := keyword.Sets{}
	for _, key := range keyword.Keys() {
		terms, err := s.ListKeywords(ctx, key)
		if err != nil {
			return nil, err
		}
		sets[key] = terms
	}
	return sets, nil
}

// AddKeywords inserts one or more terms into a list (blank terms skipped,
// duplicates ignored via the primary key). Returns how many were newly added.
func (s *Store) AddKeywords(ctx context.Context, listKey string, terms []string) (int, error) {
	if _, ok := keyword.ListByKey(listKey); !ok {
		return 0, unknownList(listKey)
	}
	added := 0
	for _, raw := range terms {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		res, err := s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO keyword_terms (list_key, term) VALUES (?, ?)", listKey, t)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}

// RemoveKeywords deletes terms from a list (case-insensitively, so the user need
// not match the stored casing). Returns how many rows were removed.
func (s *Store) RemoveKeywords(ctx context.Context, listKey string, terms []string) (int, error) {
	if _, ok := keyword.ListByKey(listKey); !ok {
		return 0, unknownList(listKey)
	}
	removed := 0
	for _, raw := range terms {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		res, err := s.db.ExecContext(ctx,
			"DELETE FROM keyword_terms WHERE list_key = ? AND lower(term) = lower(?)", listKey, t)
		if err != nil {
			return removed, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			removed += int(n)
		}
	}
	return removed, nil
}

func unknownList(key string) error {
	return &ListError{Key: key}
}

// ListError is returned for an unknown keyword list key.
type ListError struct{ Key string }

func (e *ListError) Error() string {
	return "unknown keyword list \"" + e.Key + "\" (valid: " + strings.Join(keyword.Keys(), ", ") + ")"
}
