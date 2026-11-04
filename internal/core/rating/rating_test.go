package rating

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   Config
		want Config
	}{
		{
			"empty falls back to default",
			Config{},
			Config{ABEnabled: false, Models: []string{DefaultModel}, DefaultModel: DefaultModel},
		},
		{
			"dedup + drop blanks, keep order",
			Config{Models: []string{"a", "", "a", "b"}, DefaultModel: "b"},
			Config{Models: []string{"a", "b"}, DefaultModel: "b"},
		},
		{
			"cap at MaxModels",
			Config{Models: []string{"a", "b", "c", "d"}, DefaultModel: "a"},
			Config{Models: []string{"a", "b", "c"}, DefaultModel: "a"},
		},
		{
			"default not in set → first model",
			Config{Models: []string{"a", "b"}, DefaultModel: "zzz"},
			Config{Models: []string{"a", "b"}, DefaultModel: "a"},
		},
		{
			"ab flag preserved",
			Config{ABEnabled: true, Models: []string{"x"}, DefaultModel: "x"},
			Config{ABEnabled: true, Models: []string{"x"}, DefaultModel: "x"},
		},
	}
	for _, c := range cases {
		got := Normalize(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Normalize = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Config{Models: []string{"a", "b"}, DefaultModel: "a"}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	bad := []Config{
		{Models: nil, DefaultModel: "a"},                          // none
		{Models: []string{"a", "b", "c", "d"}, DefaultModel: "a"}, // too many
		{Models: []string{"a", ""}, DefaultModel: "a"},            // empty id
		{Models: []string{"a", "a"}, DefaultModel: "a"},           // dup
		{Models: []string{"a", "b"}, DefaultModel: "zzz"},         // default not in set
	}
	for i, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("case %d: expected error, got nil for %+v", i, c)
		}
	}
}

func TestActiveModels(t *testing.T) {
	c := Config{ABEnabled: false, Models: []string{"a", "b", "c"}, DefaultModel: "b"}
	if got := c.ActiveModels(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("AB off = %v, want [b]", got)
	}
	c.ABEnabled = true
	if got := c.ActiveModels(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("AB on = %v, want all", got)
	}
}

func TestLetterForScore(t *testing.T) {
	cases := map[float64]string{
		5.0: "A", 4.5: "A", 4.49: "B", 4.0: "B", 3.99: "C",
		3.5: "C", 3.49: "D", 3.0: "D", 2.99: "F", 1.0: "F",
	}
	for score, want := range cases {
		if got := LetterForScore(score); got != want {
			t.Errorf("LetterForScore(%.2f) = %q, want %q", score, got, want)
		}
	}
}

func TestValidateVerdict(t *testing.T) {
	good := RawVerdict{JobID: 7, Score: 4.2, Letter: "b", Reasoning: "solid archetype fit here"}
	v, err := ValidateVerdict(good)
	if err != nil {
		t.Fatalf("good verdict rejected: %v", err)
	}
	if v.Letter != "B" { // upper-cased
		t.Errorf("letter not normalized: %q", v.Letter)
	}
	if v.Reasoning != "solid archetype fit here" {
		t.Errorf("reasoning not trimmed/kept: %q", v.Reasoning)
	}

	bad := []RawVerdict{
		{JobID: 0, Score: 4, Letter: "A", Reasoning: "long enough reason"},   // id
		{JobID: 1, Score: 0.5, Letter: "A", Reasoning: "long enough reason"}, // score low
		{JobID: 1, Score: 5.1, Letter: "A", Reasoning: "long enough reason"}, // score high
		{JobID: 1, Score: 4, Letter: "E", Reasoning: "long enough reason"},   // letter
		{JobID: 1, Score: 4, Letter: "A", Reasoning: "short"},                // reasoning
	}
	for i, r := range bad {
		if _, err := ValidateVerdict(r); err == nil {
			t.Errorf("bad verdict %d accepted: %+v", i, r)
		}
	}
}

func TestOwnsBoardBadge(t *testing.T) {
	if !OwnsBoardBadge("", "claude-opus-4-8", true) {
		t.Error("empty model should own badge")
	}
	if !OwnsBoardBadge("claude-code", DefaultModel, false) {
		t.Error("legacy claude-code should own badge with A/B off")
	}
	if !OwnsBoardBadge("claude-code", "claude-code", true) {
		t.Error("claude-code as the default should own badge")
	}
	if OwnsBoardBadge("claude-code", "jev-1.13.0", true) {
		t.Error("claude-code as an A/B challenger should NOT own badge")
	}
	if !OwnsBoardBadge("gemini-flash", "gemini-flash", true) {
		t.Error("default model should own badge")
	}
	if OwnsBoardBadge("kimi-k2", "gemini-flash", true) {
		t.Error("challenger should NOT own badge")
	}
}
