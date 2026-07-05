package apply

import (
	"testing"
	"time"
)

func TestClassifyApply(t *testing.T) {
	cases := []struct {
		name  string
		jd    string
		want  ApplyMethod
		email string
	}{
		{"plain email", "Gửi CV về hr@acme.vn nhé", MethodEmail, "hr@acme.vn"},
		{"strips trailing punctuation", "Liên hệ: tuyendung@acme.com.", MethodEmail, "tuyendung@acme.com"},
		{"lowercases", "Send to Recruit@Acme.VN", MethodEmail, "recruit@acme.vn"},
		{"skips noreply, takes next", "no-reply@acme.vn hoặc hr@acme.vn", MethodEmail, "hr@acme.vn"},
		{"only noreply → platform", "Auto: donotreply@acme.vn", MethodPlatform, ""},
		{"no email → platform", "Ứng tuyển trên TopCV", MethodPlatform, ""},
		{"empty → platform", "", MethodPlatform, ""},
	}
	for _, c := range cases {
		got := ClassifyApply(c.jd)
		if got.Method != c.want || got.ContactEmail != c.email {
			t.Errorf("%s: ClassifyApply = {%s,%q}, want {%s,%q}", c.name, got.Method, got.ContactEmail, c.want, c.email)
		}
	}
}

func TestPlatformOf(t *testing.T) {
	cases := map[string]Platform{
		"https://www.topcv.vn/viec-lam/x":     PlatformTopCV,
		"https://vietnamworks.com/j":          PlatformVNW,
		"https://careerlink.vn/j":             PlatformCareerLink,
		"https://vn.joboko.com/j":             PlatformJobOKO,
		"https://linkedin.com/jobs/1":         PlatformNone,
		"":                                    PlatformNone,
	}
	for url, want := range cases {
		if got := PlatformOf(url); got != want {
			t.Errorf("PlatformOf(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestValidState(t *testing.T) {
	for _, s := range []ApplyState{StateQueued, StateFilling, StateApplied, StateFailed, StateCooldown} {
		if !ValidState(s) {
			t.Errorf("ValidState(%q) = false, want true", s)
		}
	}
	for _, s := range []ApplyState{"", "done", "pending"} {
		if ValidState(s) {
			t.Errorf("ValidState(%q) = true, want false", s)
		}
	}
}

func TestCooldownRemaining(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	// never applied → clear
	if c := CooldownRemaining(time.Time{}, now); c.Remaining != 0 || c.NextAt != nil {
		t.Errorf("zero lastApply: got %+v", c)
	}
	// applied 100s ago → 200 left, NextAt set
	if c := CooldownRemaining(now.Add(-100*time.Second), now); c.Remaining != 200 || c.NextAt == nil {
		t.Errorf("100s ago: got remaining=%d nextAt=%v", c.Remaining, c.NextAt)
	} else if want := now.Add(200 * time.Second); !c.NextAt.Equal(want) {
		t.Errorf("NextAt = %v, want %v", c.NextAt, want)
	}
	// applied exactly 300s ago → clear
	if c := CooldownRemaining(now.Add(-300*time.Second), now); c.Remaining != 0 || c.NextAt != nil {
		t.Errorf("300s ago: got %+v", c)
	}
	// applied 400s ago → clear (negative clamped)
	if c := CooldownRemaining(now.Add(-400*time.Second), now); c.Remaining != 0 || c.NextAt != nil {
		t.Errorf("400s ago: got %+v", c)
	}
	// round-half-up: 299.5s elapsed → round(0.5)=1 left
	if c := CooldownRemaining(now.Add(-299500*time.Millisecond), now); c.Remaining != 1 {
		t.Errorf("299.5s ago: remaining=%d, want 1", c.Remaining)
	}
}
