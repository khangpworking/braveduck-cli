package api

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// i18nEntry matches one `"English": "Tiếng Việt",` line in a dictionary.
var i18nEntry = regexp.MustCompile(`^\s*"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)",?\s*$`)

// The same English text must not get two different translations in two
// dictionary files: the file loaded last would silently win everywhere
// (e.g. a card's "More" read "Thêm bộ lọc", the search window's "add filters").
func TestI18nKeysAgree(t *testing.T) {
	seen := map[string]string{} // key → "file: translation"
	files, _ := fs.Glob(webFS, "web/i18n-vi-*.js")
	if len(files) < 2 {
		t.Fatalf("dictionaries found: %v", files)
	}
	for _, f := range files {
		b, err := fs.ReadFile(webFS, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			m := i18nEntry.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			key, vi, where := m[1], m[2], path.Base(f)
			if prev, ok := seen[key]; ok && !strings.HasSuffix(prev, ": "+vi) {
				t.Errorf("%q is translated twice: %s and %s: %s", key, prev, where, vi)
			}
			seen[key] = where + ": " + vi
		}
	}
	if len(seen) < 50 {
		t.Fatalf("only %d entries read — has the dictionary format changed?", len(seen))
	}
}
