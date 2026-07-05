package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// GetRatingConfig reads the single-row A/B rater config, always returning a safe,
// normalized value. An absent row (fresh DB) yields the default single-rater
// config; a wedged stored value is repaired by rating.Normalize rather than
// failing, so the rater can never be blocked by bad config.
func (s *Store) GetRatingConfig(ctx context.Context) (rating.Config, error) {
	var abEnabled int
	var models, def string
	err := s.db.QueryRowContext(ctx,
		"SELECT ab_enabled, models, default_model FROM rating_config WHERE id = 1").
		Scan(&abEnabled, &models, &def)
	if errors.Is(err, sql.ErrNoRows) {
		return rating.Normalize(rating.Config{}), nil
	}
	if err != nil {
		return rating.Config{}, err
	}
	return rating.Normalize(rating.Config{
		ABEnabled:    abEnabled != 0,
		Models:       splitModels(models),
		DefaultModel: def,
	}), nil
}

// SetRatingConfig validates (strict — see rating.Config.Validate) and upserts the
// single config row. Returns the stored config read back through Normalize.
func (s *Store) SetRatingConfig(ctx context.Context, c rating.Config) (rating.Config, error) {
	if err := c.Validate(); err != nil {
		return rating.Config{}, err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rating_config (id, ab_enabled, models, default_model)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   ab_enabled = excluded.ab_enabled,
		   models = excluded.models,
		   default_model = excluded.default_model`,
		b2i(c.ABEnabled), strings.Join(c.Models, ","), c.DefaultModel)
	if err != nil {
		return rating.Config{}, err
	}
	return s.GetRatingConfig(ctx)
}

func splitModels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
