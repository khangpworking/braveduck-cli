package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
)

// Clearing the board (docs/career-page-functional-spec.md §7): a Saved or
// Waiting job whose posting closed ClearGraceDays ago, or an application with
// no answer ApplySilenceDays+ClearGraceDays after it was sent, moves to
// Closed with a note saying why. Nothing is deleted — dragging the card back
// undoes it (and keeps it) — and pinned or kept jobs are never touched.

// sweptNote starts the note the sweep adds; moving such a card out of
// Closed marks it kept (UpdateJob), so the sweep leaves it alone after.
const sweptNote = "Closed automatically:"

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
		// Only if the job is still as read: an edit or a move made meanwhile wins.
		ok, err := s.st.CloseIfUnchanged(ctx, j.ID, j.Status, j.UpdatedAt, note, now)
		if err != nil {
			return moved, err
		}
		if ok {
			moved++
		}
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
			return fmt.Sprintf("%s the posting closed on %s.", sweptNote, jobs.ParseExpiry(j.ExpiresAt).Date)
		}
	case "applied":
		if jobs.IsAppliedClearable(j.ExpiresAt, now) {
			a := jobs.AppliedStateOf(j.ExpiresAt, now)
			return fmt.Sprintf("%s no answer %d days after applying.", sweptNote, a.DaysSince)
		}
	}
	return ""
}

// keepEvents is how much activity history is kept (the feed shows the
// latest few; older lines are trimmed with each sweep).
const keepEvents = 5000

// sweepIfDue runs SweepExpired when sweepEvery has passed since the last one,
// and trims the activity feed to keepEvents.
func (s *Service) sweepIfDue(ctx context.Context, last *time.Time) {
	if now := s.now(); now.Sub(*last) >= sweepEvery {
		*last = now
		_, _ = s.SweepExpired(ctx)
		_, _ = s.st.PruneEvents(ctx, keepEvents)
	}
}
