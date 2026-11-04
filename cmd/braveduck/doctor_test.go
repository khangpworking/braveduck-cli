package main

import (
	"strings"
	"testing"

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
