package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

// loadRatings attaches verdicts to a job snapshot in bounded batches, avoiding
// a separate query per card and SQLite's parameter limit on large boards.
// Call only after closing any job rows (in-memory stores use one connection).
func (s *Store) loadRatings(ctx context.Context, list []jobs.Job) error {
	const batchSize = 500
	for start := 0; start < len(list); start += batchSize {
		batch := list[start:min(start+batchSize, len(list))]
		byID := make(map[int64]*jobs.Job, len(batch))
		args := make([]any, len(batch))
		for i := range batch {
			byID[batch[i].ID] = &batch[i]
			args[i] = batch[i].ID
		}
		rows, err := s.db.QueryContext(ctx, `SELECT job_id, model, status,
			COALESCE(score, 0), COALESCE(score_letter, ''), COALESCE(score_reasoning, '')
			FROM job_ratings WHERE job_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var model string
			var v jobs.ModelRating
			if err := rows.Scan(&id, &model, &v.Status, &v.Score, &v.Letter, &v.Reasoning); err != nil {
				rows.Close()
				return err
			}
			j := byID[id]
			if j.Ratings == nil {
				j.Ratings = make(map[string]jobs.ModelRating)
			}
			j.Ratings[model] = v
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

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
