package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Clearing the board (docs/career-page-functional-spec.md §7): a Saved or
// Waiting job whose posting closed ClearGraceDays ago, or an application with
// no answer ApplySilenceDays+ClearGraceDays after it was sent, moves to
// Closed with a note saying why. Nothing is deleted — dragging the card back
// undoes it — and pinned or kept jobs are never touched.

// sweepEvery is how often the scheduler clears the board.
const sweepEvery = time.Hour

var sweepMu sync.Mutex // one sweep at a time, per process

// SweepExpired moves expired jobs to Closed and returns how many moved.
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	sweepMu.Lock()
	defer sweepMu.Unlock()
	all, err := s.st.List(ctx, ListFilter{})
	if err != nil {
		return 0, err
	}
	now := s.now()
	moved := 0
	for _, j := range all {
		note := sweepNote(j, now)
		if note == "" {
			continue
		}
		notes := strings.TrimSpace(note + "\n" + j.Notes)
		closed := "closed"
		if err := s.st.Patch(ctx, j.ID, store.JobPatch{Status: &closed, Notes: &notes}, now); err != nil {
			return moved, err
		}
		moved++
	}
	if moved > 0 {
		s.logEvent(ctx, "moved", fmt.Sprintf("Moved %d expired job(s) to Closed (drag one back to keep it)", moved))
	}
	return moved, nil
}

// sweepNote is why j should move to Closed now, or "" to leave it.
func sweepNote(j jobs.Job, now time.Time) string {
	if j.Pinned || j.Keep {
		return ""
	}
	switch j.Status {
	case "saved", "waiting":
		if jobs.IsClearable(j.ExpiresAt, now) {
			return fmt.Sprintf("Closed automatically: the posting closed on %s.", jobs.ParseExpiry(j.ExpiresAt).Date)
		}
	case "applied":
		if jobs.IsAppliedClearable(j.ExpiresAt, now) {
			a := jobs.AppliedStateOf(j.ExpiresAt, now)
			return fmt.Sprintf("Closed automatically: no answer %d days after applying.", a.DaysSince)
		}
	}
	return ""
}

// sweepIfDue runs SweepExpired when sweepEvery has passed since the last one.
func (s *Service) sweepIfDue(ctx context.Context, last *time.Time) {
	if now := s.now(); now.Sub(*last) >= sweepEvery {
		*last = now
		_, _ = s.SweepExpired(ctx)
	}
}
