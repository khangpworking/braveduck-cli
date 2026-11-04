package rating

import (
	"errors"
	"strings"
)

// Config errors (strict write path).
var (
	errAtLeastOne      = errors.New("pick at least 1 model")
	errTooMany         = errors.New("at most 3 models")
	errEmptyID         = errors.New("model id must not be empty")
	errDuplicate       = errors.New("duplicate model id")
	errDefaultNotInSet = errors.New("default model must be one of the selected models")
)

// Verdict errors (strict verdict validation).
var (
	errBadID     = errors.New("bad id")
	errBadScore  = errors.New("score must be 1.0-5.0")
	errBadLetter = errors.New("letter must be A|B|C|D|F")
	errShortWhy  = errors.New("reasoning too short")
)

// DefaultRubric is a short, generic scoring skeleton. It is intentionally NOT
// personalized — braveduck-cli users override it with their own profile/rubric
// (the original app's rubric encoded one candidate's comp targets and archetypes,
// which don't belong in a shared tool). The letter thresholds below are the one
// universal piece and match LetterForScore.
const DefaultRubric = "Score the role for THIS candidate on a 1.0-5.0 global scale, a weighted " +
	"average across: (1) CV/skills match, (2) alignment with the candidate's stated " +
	"target roles, (3) compensation vs the candidate's market, (4) cultural signals " +
	"(growth, stability, remote policy), (5) red flags as negative adjustments. " +
	"Letters: A=4.5+ strong (apply now), B=4.0-4.4 good, C=3.5-3.9 decent, " +
	"D=3.0-3.4 weak, F<3.0 skip. Reasoning: 3-4 concise points on a single line, " +
	"separated by \" • \" — archetype fit, strongest matching proof point, comp if " +
	"notable, and the key gap or red flag. Be honest: score low when core required " +
	"skills are missing, even if the title sounds appealing."

// LetterForScore maps a 1.0-5.0 global score to its badge letter, using the same
// thresholds as the rubric. Scores outside the range still map by the nearest
// band (>=4.5 → A at the top; anything under 3.0 → F).
func LetterForScore(score float64) string {
	switch {
	case score >= 4.5:
		return "A"
	case score >= 4.0:
		return "B"
	case score >= 3.5:
		return "C"
	case score >= 3.0:
		return "D"
	default:
		return "F"
	}
}

// Verdict is a validated rating result from some rater model.
type Verdict struct {
	JobID     int64
	Score     float64
	Letter    string
	Reasoning string
}

// RawVerdict is the loosely-typed input from a CLI arg, HTTP body or MCP call.
type RawVerdict struct {
	JobID     int64
	Score     float64
	Letter    string
	Reasoning string
}

// ValidateVerdict enforces the same rules as the original validateVerdict so
// every transport (CLI/API/MCP) rejects bad input identically: positive id,
// score in [1,5], letter ∈ A|B|C|D|F (upper-cased), reasoning ≥ 10 chars. The
// model's own letter is trusted — use LetterForScore if you'd rather derive it.
func ValidateVerdict(raw RawVerdict) (Verdict, error) {
	if raw.JobID <= 0 {
		return Verdict{}, errBadID
	}
	if !(raw.Score >= 1 && raw.Score <= 5) {
		return Verdict{}, errBadScore
	}
	letter := strings.ToUpper(strings.TrimSpace(raw.Letter))
	if len(letter) != 1 || !strings.ContainsRune("ABCDF", rune(letter[0])) {
		return Verdict{}, errBadLetter
	}
	reasoning := strings.TrimSpace(raw.Reasoning)
	if len(reasoning) < 10 {
		return Verdict{}, errShortWhy
	}
	return Verdict{JobID: raw.JobID, Score: raw.Score, Letter: letter, Reasoning: reasoning}, nil
}

// OwnsBoardBadge reports whether a verdict from model should also become the
// board's canonical badge. The empty model and the legacy "claude-code" tag
// always mirror (the pre-A/B single-rater path); otherwise only the configured
// default rater owns the badge.
func OwnsBoardBadge(model, defaultModel string) bool {
	if model == "" || model == "claude-code" {
		return true
	}
	return model == defaultModel
}
