package main

import (
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

func TestSetupLines(t *testing.T) {
	got := strings.Join(setupLines(service.SetupStatus{AboutMe: true, SearchQueries: 5}), "\n")
	for _, want := range []string{
		"✓ CV / about me\n",
		"✗ what you want   edit the profile",
		"✓ search terms    5",
		"✗ TopCV login     optional: braveduck login topcv",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, " \n") {
		t.Errorf("trailing spaces:\n%q", got)
	}
}

func TestRatedLine(t *testing.T) {
	j := jobs.Job{ID: 7, ScoreLetter: "B", Score: 3.9, Ratings: map[string]jobs.ModelRating{
		"jev-1.13.0": {Letter: "B", Score: 3.9}, "rival-one": {Letter: "F", Score: 2.0},
	}}
	if got := ratedLine(j, "rival-one"); got != "job #7: rival-one rated F 2.0 (the board's badge stays B 3.9)" {
		t.Errorf("challenger = %q", got)
	}
	if got := ratedLine(j, ""); got != "job #7 rated B 3.9" {
		t.Errorf("default = %q", got)
	}
}
