package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// SaveRating records model's verdict for a job in job_ratings and, when
// ownsBadge, also makes it the job's board badge (score, letter, reasoning,
// score_status = scored). Both writes land in one transaction. Returns
// sql.ErrNoRows if the job doesn't exist.
func (s *Store) SaveRating(ctx context.Context, model string, v rating.Verdict, ownsBadge bool, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ts := now.UTC().Format(time.RFC3339)
	var res sql.Result
	if ownsBadge {
		res, err = tx.ExecContext(ctx,
			`UPDATE jobs SET score = ?, score_letter = ?, score_status = 'scored',
			   score_reasoning = ?, updated_at = ? WHERE id = ?`,
			v.Score, v.Letter, v.Reasoning, ts, v.JobID)
	} else {
		// Challenger verdicts don't touch the badge, but still bump updated_at
		// so the board refreshes.
		res, err = tx.ExecContext(ctx, `UPDATE jobs SET updated_at = ? WHERE id = ?`, ts, v.JobID)
	}
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job_ratings (job_id, model, status, score, score_letter, score_reasoning)
		 VALUES (?, ?, 'scored', ?, ?, ?)
		 ON CONFLICT(job_id, model) DO UPDATE SET
		   status = 'scored', score = excluded.score,
		   score_letter = excluded.score_letter, score_reasoning = excluded.score_reasoning`,
		v.JobID, model, v.Score, v.Letter, v.Reasoning); err != nil {
		return err
	}
	return tx.Commit()
}
