// Package rating holds the portable job-scoring control logic: the A/B rater
// configuration rules, the 1.0-5.0 → A-F letter scale, and verdict validation.
//
// This is a faithful port of the proven logic in the original personal-assistant
// app (src/lib/ratingConfig.ts and src/services/rating.ts). The mechanism is
// intentionally identical — 1-3 raters score the queue, exactly one owns the
// board badge, A/B-off collapses to the single default rater. Two things change
// for the public tool: the model roster is OPEN (any harness supplies its own id
// string, instead of a hardcoded private cliproxy alias list), and the scoring
// rubric is a short generic skeleton users override, not the author's personal
// career profile.
package rating

// DefaultModel is the out-of-box rater id used when no config has been saved.
// Users point this at whatever harness they wire up.
const DefaultModel = "claude-opus-4-8"

// MaxModels caps how many raters can score the queue at once (A/B/C fan-out).
const MaxModels = 3

// Config is the live A/B rating configuration: which model ids score the queue,
// which one owns the board's canonical badge, and whether the challengers run.
// A/B-off means only DefaultModel-of-the-set scores — behaviourally identical to
// a single rater.
type Config struct {
	ABEnabled    bool
	Models       []string // 1..MaxModels rater ids that score the queue
	DefaultModel string   // ∈ Models; owns the board badge
}

// Normalize repairs a raw/stored config into a safe, usable one, matching the
// original getRatingConfig fallbacks: blank ids dropped, duplicates collapsed,
// capped at MaxModels, at least one model (falls back to DefaultModel), and the
// default forced into the set (first model wins if it isn't). Never errors — it
// is the read path, so a wedged value can never stop the rater.
func Normalize(raw Config) Config {
	seen := map[string]bool{}
	var models []string
	for _, id := range raw.Models {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
		if len(models) == MaxModels {
			break
		}
	}
	if len(models) == 0 {
		models = []string{DefaultModel}
	}

	def := raw.DefaultModel
	if def == "" || !contains(models, def) {
		def = models[0]
	}
	return Config{ABEnabled: raw.ABEnabled, Models: models, DefaultModel: def}
}

// Validate is the strict write path (the original setRatingConfig checks): reject
// rather than repair, so a caller saving config learns exactly what's wrong.
func (c Config) Validate() error {
	if len(c.Models) < 1 {
		return errAtLeastOne
	}
	if len(c.Models) > MaxModels {
		return errTooMany
	}
	seen := map[string]bool{}
	for _, id := range c.Models {
		if id == "" {
			return errEmptyID
		}
		if seen[id] {
			return errDuplicate
		}
		seen[id] = true
	}
	if !contains(c.Models, c.DefaultModel) {
		return errDefaultNotInSet
	}
	return nil
}

// ActiveModels are the ids that actually score right now: every selected model
// when A/B is on, else just the default — collapsing to the single-rater path.
func (c Config) ActiveModels() []string {
	if c.ABEnabled {
		return c.Models
	}
	return []string{c.DefaultModel}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
