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
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Company  string `json:"company"`
	URL      string `json:"url"`
	Location string `json:"location"`

	Status string `json:"status"` // saved / waiting / applied / interview / offer / closed
	Pinned bool   `json:"pinned"`
	Keep   bool   `json:"keep"`
	Notes  string `json:"notes"`

	Salary    string `json:"salary"`
	Skills    string `json:"skills"` // comma-separated
	Summary   string `json:"jd_summary"`
	AISummary string `json:"jd_ai_summary"`
	FullJD    string `json:"jd_full"`

	DiscoverySource string `json:"discovery_source"` // topcv/vietnamworks/joboko/careerlink/serpapi/linkedin/paste

	// Scoring (top-level, mirrors the winning A/B model's row).
	Score          float64                `json:"score"`
	HasScore       bool                   `json:"-"` // distinguishes score 0 from "not scored"
	ScoreLetter    string                 `json:"score_letter"`
	ScoreStatus    string                 `json:"score_status"` // pending/scored/failed
	ScoreReasoning string                 `json:"score_reasoning"`
	Ratings        map[string]ModelRating `json:"ratings,omitempty"`
	Dealbreakers   []string               `json:"dealbreakers,omitempty"` // computed by the service (Dealbreakers), not stored
	Clock          *Clock                 `json:"clock,omitempty"`        // computed by the service (ClockOf), not stored

	// Apply pipeline.
	ApplyMethod    string `json:"apply_method"` // email/platform
	ApplyState     string `json:"apply_state"`  // queued/filling/cooldown/failed/applied
	ApplyNextAt    string `json:"apply_next_at"`
	ApplyDetail    string `json:"apply_detail"`
	ApplyUpdatedAt string `json:"apply_updated_at"`

	// CV generation.
	CVPath        string `json:"cv_path"`
	CVStatus      string `json:"cv_status"`
	CVVersion     int    `json:"cv_version"`
	CVGeneratedAt string `json:"cv_generated_at"`

	// Email draft.
	DraftEmail    string `json:"draft_email"`
	DraftStatus   string `json:"draft_status"`
	DraftStatusAt string `json:"draft_status_at"`
	DraftNotes    string `json:"draft_notes"`
	ContactEmail  string `json:"contact_email"`

	// Timestamps / lifecycle.
	ExpiresAt      string `json:"expires_at"` // the expires_at grammar value (see parseExpiry)
	FollowupSentAt string `json:"followup_sent_at"`
	SavedAt        string `json:"saved_at"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`

	// Per-platform recruiter-view status sync.
	TopcvStatus string `json:"topcv_status"`
	VnwStatus   string `json:"vnw_status"`

	// Geo/company signals used by geoDealbreakers. Resolve async on pin.
	CompanyActiveDate   string  `json:"company_active_date"` // masothue "Ngày hoạt động", YYYY-MM-DD or a sentinel
	CompanyDistanceKM   float64 `json:"company_distance_km"` // meaningful only when CompanyGeoStatus == "ok"
	HasDistance         bool    `json:"-"`                   // distinguishes 0 km from "not set"
	CompanyGeoStatus    string  `json:"company_geo_status"`  // "" / pending / ok / no_address / geocode_failed
	CompanyMasothueURL  string  `json:"company_masothue_url"`
	CompanyMasothueName string  `json:"company_masothue_name"`
}

// ModelRating is a saved per-model verdict; only the configured default
// model's verdict also owns the job's top-level board badge.
type ModelRating struct {
	Status    string  `json:"status"`
	Score     float64 `json:"score"`
	Letter    string  `json:"letter"`
	Reasoning string  `json:"reasoning"`
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
