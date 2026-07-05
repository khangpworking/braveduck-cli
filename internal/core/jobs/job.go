// Package jobs holds the core, portable domain logic for the job board:
// the expires_at value grammar, the deadline/apply-silence clocks, dealbreaker
// detection, and the interview-rate ("morale") calculation.
//
// This is a faithful Go port of the proven TypeScript logic in the original
// personal-assistant app (src/lib/jobExpiry.ts and the helpers in
// src/views/learning.tsx). The behavior is intentionally identical; only two
// things change to suit Go: the "current time" is passed in explicitly (instead
// of a global Date.now()) so the clocks are deterministically testable, and the
// dealbreaker regexes are compiled once at package init.
package jobs

import (
	"strings"
	"time"
)

// Job is the canonical job-listing record — a faithful port of the persisted
// job_listings row (see ARCHITECTURE.md §2.3 / internal/store/schema.sql). The pure
// logic in this package only ever touches a subset (status, salary, the JD text,
// the morale timestamps, and the geo/company signals); the remaining fields are
// carried so the store, CLI, API and MCP layers share one model with no mapping.
type Job struct {
	ID      int64
	Title   string
	Company string
	URL     string
	Location string

	Status  string // saved / waiting / applied / interview / offer / closed
	Pinned  bool
	Keep    bool
	Notes   string

	Salary    string
	Skills    string // comma-separated
	Summary   string // jd_summary
	AISummary string // jd_ai_summary
	FullJD    string // jd_full

	DiscoverySource string // topcv/vietnamworks/joboko/careerlink/serpapi/linkedin/paste

	// Scoring (top-level, mirrors the winning A/B model's row).
	Score          float64
	HasScore       bool // distinguishes score 0 from "not scored"
	ScoreLetter    string
	ScoreStatus    string // pending/scored/failed
	ScoreReasoning string

	// Apply pipeline.
	ApplyMethod    string // email/platform
	ApplyState     string // queued/filling/cooldown/failed/applied
	ApplyNextAt    string
	ApplyDetail    string
	ApplyUpdatedAt string

	// CV generation.
	CVPath        string
	CVStatus      string
	CVVersion     int
	CVGeneratedAt string

	// Email draft.
	DraftEmail    string
	DraftStatus   string
	DraftStatusAt string
	DraftNotes    string
	ContactEmail  string

	// Timestamps / lifecycle.
	ExpiresAt      string // the expires_at grammar value (see parseExpiry)
	FollowupSentAt string
	SavedAt        string
	CreatedAt      string
	UpdatedAt      string

	// Per-platform recruiter-view status sync.
	TopcvStatus string
	VnwStatus   string

	// Geo/company signals used by geoDealbreakers. Resolve async on pin.
	CompanyActiveDate   string  // masothue "Ngày hoạt động", YYYY-MM-DD or a sentinel
	CompanyDistanceKM   float64 // meaningful only when CompanyGeoStatus == "ok"
	HasDistance         bool    // distinguishes 0 km from "not set"
	CompanyGeoStatus    string  // "" / pending / ok / no_address / geocode_failed
	CompanyMasothueURL  string
	CompanyMasothueName string
}

// parseTime decodes the timestamp formats that appear across the DB: full
// RFC3339 (saved_at etc.), the SQLite naive "YYYY-MM-DD HH:MM:SS" form, and a
// bare "YYYY-MM-DD" date. Naive values are read as UTC — matching the day-
// granularity thresholds the clocks use, where a few hours of zone offset never
// crosses a boundary that mattered in the original app.
func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

const dayMS = float64(86_400_000)

func floorDiv(ms float64) int { return int(floor(ms / dayMS)) }
func ceilDiv(ms float64) int  { return int(ceil(ms / dayMS)) }

// Small float helpers to mirror JS Math.floor/Math.ceil exactly (round toward
// -Inf / +Inf), avoiding a math import for two calls.
func floor(f float64) float64 {
	i := float64(int64(f))
	if f < 0 && i != f {
		i--
	}
	return i
}
func ceil(f float64) float64 {
	i := float64(int64(f))
	if f > 0 && i != f {
		i++
	}
	return i
}
