package jobs

import (
	"testing"
	"time"
)

// Fixed reference "now" so every clock test is deterministic.
var now = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func TestParseExpiry(t *testing.T) {
	cases := []struct {
		raw  string
		kind ExpiryKind
		date string
	}{
		{"", KindNone, ""},
		{"unknown", KindUnknown, ""},
		{"expire:2026-07-15", KindDeadline, "2026-07-15"},
		{"applied:2026-06-01", KindApplied, "2026-06-01"},
		{"2026-07-15", KindDeadline, "2026-07-15"}, // legacy bare ISO
		{"garbage", KindUnknown, ""},
	}
	for _, c := range cases {
		got := ParseExpiry(c.raw)
		if got.Kind != c.kind || got.Date != c.date {
			t.Errorf("ParseExpiry(%q) = {%s,%q}, want {%s,%q}", c.raw, got.Kind, got.Date, c.kind, c.date)
		}
	}
}

func TestExpiryState(t *testing.T) {
	cases := []struct {
		raw      string
		state    ExpiryState
		daysLeft int
	}{
		{"expire:2026-07-05", StateExpired, 0},  // today = expired (daysLeft<=0)
		{"expire:2026-07-04", StateExpired, -1}, // past
		{"expire:2026-07-15", StateWarning, 10}, // exactly WarningDays
		{"expire:2026-07-16", StateOK, 11},      // beyond warning
		{"unknown", StateUnknown, 0},
		{"applied:2026-06-01", StateUnknown, 0}, // applied anchor isn't a deadline
		{"", StateUnknown, 0},
	}
	for _, c := range cases {
		got := ExpiryStateOf(c.raw, now)
		if got.State != c.state {
			t.Errorf("ExpiryStateOf(%q).State = %s, want %s", c.raw, got.State, c.state)
		}
		if got.State != StateUnknown && got.DaysLeft != c.daysLeft {
			t.Errorf("ExpiryStateOf(%q).DaysLeft = %d, want %d", c.raw, got.DaysLeft, c.daysLeft)
		}
	}
}

func TestIsClearable(t *testing.T) {
	if !IsClearable("expire:2026-06-28", now) { // -7 days → clearable
		t.Error("expire 7 days past should be clearable")
	}
	if IsClearable("expire:2026-06-29", now) { // -6 days → not yet
		t.Error("expire 6 days past should NOT be clearable")
	}
	if IsClearable("unknown", now) {
		t.Error("unknown should never be clearable")
	}
}

func TestAppliedState(t *testing.T) {
	// since<20 → silent
	if a := AppliedStateOf("applied:2026-07-01", now); !a.OK || a.State != ClockSilent || a.DaysSince != 4 {
		t.Errorf("recent apply: got %+v", a)
	}
	// since==20 → boundary: not silent, still counting down
	if a := AppliedStateOf("applied:2026-06-15", now); a.State != ClockCountdown || a.DaysSince != 20 || a.DaysToDelete != 7 {
		t.Errorf("20-day apply: got %+v", a)
	}
	// since>27 → expired
	if a := AppliedStateOf("applied:2026-06-01", now); a.State != ClockExpired {
		t.Errorf("34-day apply should be expired: got %+v", a)
	}
	if !IsAppliedClearable("applied:2026-06-01", now) {
		t.Error("34-day silent apply should be clearable")
	}
	if IsAppliedClearable("applied:2026-07-01", now) {
		t.Error("4-day apply should NOT be clearable")
	}
	// non-applied value
	if a := AppliedStateOf("expire:2026-07-15", now); a.OK {
		t.Error("deadline value should not yield an applied clock")
	}
}

func TestIsPlatformJob(t *testing.T) {
	yes := []string{"https://www.topcv.vn/viec-lam/x", "https://vietnamworks.com/j", "https://vn.joboko.com/j", "https://careerlink.vn/j"}
	no := []string{"", "https://example.com", "https://linkedin.com/jobs/1"}
	for _, u := range yes {
		if !IsPlatformJob(u) {
			t.Errorf("IsPlatformJob(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if IsPlatformJob(u) {
			t.Errorf("IsPlatformJob(%q) = true, want false", u)
		}
	}
}

func TestSuccessRate(t *testing.T) {
	js := []Job{
		{Status: "interview"},
		{Status: "interview"},
		{Status: "offer"},
		{Status: "closed"},
		{Status: "applied", ExpiresAt: "applied:2026-06-01"}, // 34d old → resolved
		{Status: "applied", ExpiresAt: "applied:2026-07-01"}, // 4d → pending
		{Status: "saved"},                                    // ignored
	}
	sr := SuccessRateOf(js, now)
	if sr.Interviews != 3 || sr.Resolved != 5 || sr.Pending != 1 {
		t.Fatalf("counts: got %+v", sr)
	}
	if sr.Rate < 0.599 || sr.Rate > 0.601 {
		t.Errorf("rate = %v, want 0.6", sr.Rate)
	}
}

func TestMoraleMessage(t *testing.T) {
	if MoraleMessage(SuccessRate{Resolved: 2}).Color != "var(--muted)" {
		t.Error("under 3 resolved → seedling")
	}
	if MoraleMessage(SuccessRate{Resolved: 10, Rate: 0.05}).Color != "#f97316" {
		t.Error("<10% → orange")
	}
	if MoraleMessage(SuccessRate{Resolved: 10, Rate: 0.2}).Color != "#84cc16" {
		t.Error("10-30% → lime")
	}
	if MoraleMessage(SuccessRate{Resolved: 10, Rate: 0.6}).Color != "#22c55e" {
		t.Error(">=30% → superstar green")
	}
}

func TestTextDealbreakers(t *testing.T) {
	has := func(ss []string, want string) bool {
		for _, s := range ss {
			if s == want {
				return true
			}
		}
		return false
	}
	// 6-day week, no off-signal → flagged
	if got := TextDealbreakers(Job{Title: "NV kho, làm 6 ngày/tuần"}); !has(got, "Làm T7") {
		t.Errorf("6-day week should flag Làm T7: %v", got)
	}
	// 6-day mention but explicit Saturday-off → suppressed
	if got := TextDealbreakers(Job{FullJD: "làm 6 ngày/tuần, nhưng nghỉ thứ 7 tuần cuối"}); has(got, "Làm T7") {
		t.Errorf("Saturday-off should suppress: %v", got)
	}
	// 5-day range PLUS explicit "và sáng Thứ 7" → satExtra overrides → flagged
	if got := TextDealbreakers(Job{FullJD: "Thời gian: Thứ 2 - Thứ 6 và sáng Thứ 7"}); !has(got, "Làm T7") {
		t.Errorf("5-day + extra Saturday should flag: %v", got)
	}
	// negotiable pay in salary field
	if got := TextDealbreakers(Job{Salary: "Thỏa thuận"}); !has(got, "Lương thỏa thuận") {
		t.Errorf("Thỏa thuận should flag: %v", got)
	}
	if got := TextDealbreakers(Job{Salary: "Competitive"}); !has(got, "Lương thỏa thuận") {
		t.Errorf("Competitive should flag: %v", got)
	}
	// clean job → nothing
	if got := TextDealbreakers(Job{Title: "Kế toán", Salary: "15 triệu", FullJD: "Thứ 2 - Thứ 6"}); len(got) != 0 {
		t.Errorf("clean job should have no dealbreakers: %v", got)
	}
}

func TestGeoDealbreakers(t *testing.T) {
	has := func(ss []string, want string) bool {
		for _, s := range ss {
			if s == want {
				return true
			}
		}
		return false
	}
	// >10km, within cap → "Xa 12km"
	if got := GeoDealbreakers(Job{CompanyGeoStatus: "ok", CompanyDistanceKM: 12, HasDistance: true}, now); !has(got, "Xa 12km") {
		t.Errorf("12km should flag: %v", got)
	}
	// >80km → wrong-province geocode, suppressed
	if got := GeoDealbreakers(Job{CompanyGeoStatus: "ok", CompanyDistanceKM: 90, HasDistance: true}, now); len(got) != 0 {
		t.Errorf("90km should be suppressed: %v", got)
	}
	// close → none
	if got := GeoDealbreakers(Job{CompanyGeoStatus: "ok", CompanyDistanceKM: 5, HasDistance: true}, now); len(got) != 0 {
		t.Errorf("5km should be fine: %v", got)
	}
	// company under 2 years old
	if got := GeoDealbreakers(Job{CompanyActiveDate: "2025-01-01"}, now); !has(got, "Cty <2 năm") {
		t.Errorf("1.5yr-old company should flag: %v", got)
	}
	// established company
	if got := GeoDealbreakers(Job{CompanyActiveDate: "2020-01-01"}, now); has(got, "Cty <2 năm") {
		t.Errorf("6yr-old company should not flag: %v", got)
	}
}
