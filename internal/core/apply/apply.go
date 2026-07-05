// Package apply ports the pure, offline-verifiable skeleton of the auto-apply
// pipeline: how a job is classified (email vs platform), which platform an apply
// URL routes to, the apply_state grammar, and the per-platform cooldown clock.
//
// These are faithful ports of applyMethod.ts (classifyApply) and the routing /
// cooldown / state logic in scripts/apply-pending.ts. The parts that actually
// drive a bot-protected apply form live in the session/apply-engine layer
// (chromedp + the user's captured session) and are deliberately NOT here — this
// package is the deterministic control logic those engines plug into, testable
// with no browser and no network.
package apply

import (
	"regexp"
	"strings"
	"time"
)

// CooldownSeconds is the per-platform gap enforced between two applies on the
// same portal (mirrors COOLDOWN_S in apply-pending.ts / the apply skills).
const CooldownSeconds = 300

// ---- Apply method classification (applyMethod.ts) -------------------------

// ApplyMethod is how you actually submit: send the CV to an address found in the
// JD, or do the application yourself on a portal.
type ApplyMethod string

const (
	MethodEmail    ApplyMethod = "email"
	MethodPlatform ApplyMethod = "platform"
)

var (
	emailRE = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	// noreply-type local parts bounce by definition, so they don't count as a
	// real apply channel.
	noreplyRE = regexp.MustCompile(`(?i)^(no-?reply|do-?not-?reply|donotreply|mailer-daemon|postmaster)`)
	// trailing punctuation to strip off a matched address, mirroring /[.,;:]+$/.
	trailingPunctRE = regexp.MustCompile(`[.,;:]+$`)
)

// Classification is the result of inspecting a JD for a usable contact address.
type Classification struct {
	Method       ApplyMethod
	ContactEmail string // "" when Method is platform
}

// ClassifyApply returns email-apply (with the first usable address) when the JD
// contains a non-noreply email, otherwise platform. First usable email wins.
func ClassifyApply(jdText string) Classification {
	for _, raw := range emailRE.FindAllString(jdText, -1) {
		email := strings.ToLower(trailingPunctRE.ReplaceAllString(raw, ""))
		local := email
		if i := strings.IndexByte(email, '@'); i >= 0 {
			local = email[:i]
		}
		if noreplyRE.MatchString(local) {
			continue
		}
		return Classification{Method: MethodEmail, ContactEmail: email}
	}
	return Classification{Method: MethodPlatform}
}

// ---- Platform routing (platformOf) ----------------------------------------

// Platform identifies which portal's apply engine handles a job's URL.
type Platform string

const (
	PlatformNone       Platform = ""
	PlatformTopCV      Platform = "topcv"
	PlatformVNW        Platform = "vnw"
	PlatformCareerLink Platform = "careerlink"
	PlatformJobOKO     Platform = "joboko"
)

// PlatformOf maps an apply URL to its platform, or PlatformNone when the URL is
// empty or on a portal with no apply engine. Order matches platformOf().
func PlatformOf(url string) Platform {
	switch {
	case url == "":
		return PlatformNone
	case strings.Contains(url, "topcv.vn"):
		return PlatformTopCV
	case strings.Contains(url, "vietnamworks.com"):
		return PlatformVNW
	case strings.Contains(url, "careerlink.vn"):
		return PlatformCareerLink
	case strings.Contains(url, "joboko.com"):
		return PlatformJobOKO
	default:
		return PlatformNone
	}
}

// ---- Apply-state grammar --------------------------------------------------

// ApplyState is the badge/queue state a platform job moves through:
// queued -> filling -> applied|failed, with cooldown as the "wait it out" state.
type ApplyState string

const (
	StateQueued   ApplyState = "queued"
	StateFilling  ApplyState = "filling"
	StateApplied  ApplyState = "applied"
	StateFailed   ApplyState = "failed"
	StateCooldown ApplyState = "cooldown"
)

var validStates = map[ApplyState]bool{
	StateQueued:   true,
	StateFilling:  true,
	StateApplied:  true,
	StateFailed:   true,
	StateCooldown: true,
}

// ValidState reports whether s is one of the accepted apply_state values
// (mirrors the `valid` guard in apply-pending.ts set).
func ValidState(s ApplyState) bool { return validStates[s] }

// ---- Cooldown clock (cooldownRemaining) -----------------------------------

// Cooldown is the remaining wait on a platform's apply cooldown.
type Cooldown struct {
	Remaining int        // seconds left, 0 when clear
	NextAt    *time.Time // when the cooldown clears, nil when already clear
}

// CooldownRemaining computes the wait left given when the last apply on that
// platform happened. A zero lastApply (never applied) is always clear. Mirrors
// remaining = max(0, round(COOLDOWN_S - (now - last))); NextAt = last+cooldown
// only while remaining > 0.
func CooldownRemaining(lastApply, now time.Time) Cooldown {
	if lastApply.IsZero() {
		return Cooldown{}
	}
	elapsed := now.Sub(lastApply).Seconds()
	remaining := round(float64(CooldownSeconds) - elapsed)
	if remaining < 0 {
		remaining = 0
	}
	if remaining == 0 {
		return Cooldown{}
	}
	next := lastApply.Add(CooldownSeconds * time.Second)
	return Cooldown{Remaining: remaining, NextAt: &next}
}

// round mirrors JS Math.round (round half up toward +Inf).
func round(x float64) int {
	return int(floorFloat(x + 0.5))
}

func floorFloat(x float64) float64 {
	n := int64(x)
	if float64(n) > x {
		n--
	}
	return float64(n)
}
