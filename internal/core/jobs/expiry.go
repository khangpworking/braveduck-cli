package jobs

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// expires_at value grammar (port of jobExpiry.ts):
//
//	""                    never checked / non-platform job, not applied
//	"unknown"             checked but no deadline found / page blocked
//	"expire:YYYY-MM-DD"   real scraped application deadline
//	"applied:YYYY-MM-DD"  no real deadline, but the card was applied on this date
//	                      — the apply-silence clock counts from here
//
// The expire:/applied: keywords disambiguate the two date meanings sharing the
// field. parseExpiry is the only thing that knows the grammar.

// Warn once the deadline is this close; hard-clear once it's this far past.
const (
	WarningDays      = 10
	ClearGraceDays   = 7
	ApplySilenceDays = 20 // silence after applying before the delete countdown begins (delete at 20+7=27)
)

const (
	ExpirePrefix  = "expire:"
	AppliedPrefix = "applied:"
)

// Listing hosts whose pages expose a scrapeable deadline. Anything else stays
// unset (renders "unknown"); we never fetch it.
var platformHostRE = regexp.MustCompile(`(?i)(joboko\.com|vietnamworks\.com|careerlink\.vn|topcv\.vn)`)

// IsPlatformJob reports whether a URL is on a host we can scrape a deadline from.
func IsPlatformJob(url string) bool {
	return url != "" && platformHostRE.MatchString(url)
}

type ExpiryKind string

const (
	KindNone     ExpiryKind = "none"
	KindUnknown  ExpiryKind = "unknown"
	KindDeadline ExpiryKind = "deadline"
	KindApplied  ExpiryKind = "applied"
)

type ParsedExpiry struct {
	Kind ExpiryKind
	Date string // bare date for deadline/applied, else ""
}

var bareISORE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// ParseExpiry is the single decoder for the expires_at grammar above.
func ParseExpiry(raw string) ParsedExpiry {
	if raw == "" {
		return ParsedExpiry{Kind: KindNone}
	}
	if raw == "unknown" {
		return ParsedExpiry{Kind: KindUnknown}
	}
	if strings.HasPrefix(raw, ExpirePrefix) {
		return ParsedExpiry{Kind: KindDeadline, Date: raw[len(ExpirePrefix):]}
	}
	if strings.HasPrefix(raw, AppliedPrefix) {
		return ParsedExpiry{Kind: KindApplied, Date: raw[len(AppliedPrefix):]}
	}
	// Legacy bare ISO (pre-migration row) — treat as a real deadline.
	if bareISORE.MatchString(raw) {
		return ParsedExpiry{Kind: KindDeadline, Date: raw}
	}
	return ParsedExpiry{Kind: KindUnknown}
}

// daysUntil: whole days from now to the date (ceil; negative once past). A
// bare YYYY-MM-DD is a calendar day in now's time zone, so a Vietnamese
// deadline of "2 Nov" isn't "past" from 07:00 on 2 Nov (UTC midnight).
func daysUntil(dateStr string, now time.Time) (int, bool) {
	if d, ok := calendarDays(dateStr, now); ok {
		return d, true
	}
	t, ok := parseTime(dateStr)
	if !ok {
		return 0, false
	}
	return ceilDiv(float64(t.Sub(now).Milliseconds())), true
}

// daysSince: whole days from the date to now (floor), calendar days for a
// bare YYYY-MM-DD as in daysUntil.
func daysSince(dateStr string, now time.Time) (int, bool) {
	if d, ok := calendarDays(dateStr, now); ok {
		return -d, true
	}
	t, ok := parseTime(dateStr)
	if !ok {
		return 0, false
	}
	return floorDiv(float64(now.Sub(t).Milliseconds())), true
}

// calendarDays is date minus today, in whole calendar days in now's zone; ok
// is false unless dateStr is a bare YYYY-MM-DD.
func calendarDays(dateStr string, now time.Time) (int, bool) {
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateStr), now.Location())
	if err != nil {
		return 0, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return int(math.Round(d.Sub(today).Hours() / 24)), true // DST days aren't 24 h
}

type ExpiryState string

const (
	StateUnknown ExpiryState = "unknown"
	StateOK      ExpiryState = "ok"
	StateWarning ExpiryState = "warning"
	StateExpired ExpiryState = "expired"
)

type ExpiryInfo struct {
	State    ExpiryState
	DaysLeft int  // whole days until deadline; negative = past
	HasDays  bool // false when unknown
}

// ExpiryState is the real-deadline state. Only expire: values yield
// ok/warning/expired; anything else collapses to unknown.
func ExpiryStateOf(raw string, now time.Time) ExpiryInfo {
	p := ParseExpiry(raw)
	if p.Kind != KindDeadline || p.Date == "" {
		return ExpiryInfo{State: StateUnknown}
	}
	daysLeft, ok := daysUntil(p.Date, now)
	if !ok {
		return ExpiryInfo{State: StateUnknown}
	}
	var state ExpiryState
	switch {
	case daysLeft <= 0:
		state = StateExpired
	case daysLeft <= WarningDays:
		state = StateWarning
	default:
		state = StateOK
	}
	return ExpiryInfo{State: state, DaysLeft: daysLeft, HasDays: true}
}

type AppliedClock string

const (
	ClockSilent    AppliedClock = "silent"
	ClockCountdown AppliedClock = "countdown"
	ClockExpired   AppliedClock = "expired"
)

type AppliedInfo struct {
	State        AppliedClock
	DaysSince    int  // days since the application was sent
	DaysToDelete int  // days remaining before the sweep deletes it (<=0 = due)
	OK           bool // false when raw is not an applied: anchor
}

// AppliedStateOf is the apply-silence clock for unknown-deadline cards: counts
// ApplySilenceDays of silence after applying, then the usual ClearGraceDays
// countdown to deletion. OK is false for any value that isn't an applied: anchor.
func AppliedStateOf(raw string, now time.Time) AppliedInfo {
	p := ParseExpiry(raw)
	if p.Kind != KindApplied || p.Date == "" {
		return AppliedInfo{}
	}
	since, ok := daysSince(p.Date, now)
	if !ok {
		return AppliedInfo{}
	}
	daysToDelete := ApplySilenceDays + ClearGraceDays - since
	var state AppliedClock
	switch {
	case since < ApplySilenceDays:
		state = ClockSilent
	case daysToDelete > 0:
		state = ClockCountdown
	default:
		state = ClockExpired
	}
	return AppliedInfo{State: state, DaysSince: since, DaysToDelete: daysToDelete, OK: true}
}

// IsClearable is true once a real deadline is ClearGraceDays or more in the past.
func IsClearable(raw string, now time.Time) bool {
	info := ExpiryStateOf(raw, now)
	return info.HasDays && info.DaysLeft <= -ClearGraceDays
}

// IsAppliedClearable is true once an applied-anchor card has gone silent past
// 20+7 days. (The sweep additionally gates this on status=='applied' —
// interview/offer cards exempt.)
func IsAppliedClearable(raw string, now time.Time) bool {
	a := AppliedStateOf(raw, now)
	return a.OK && a.State == ClockExpired
}
