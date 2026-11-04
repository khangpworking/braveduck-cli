package browser

import (
	"strings"
	"testing"
)

func TestRaiseScriptQuotesTitleParts(t *testing.T) {
	s := raiseScript([]string{"Chromium", "(it's'); Remove-Item C:\\ -Recurse; ('"})
	if !strings.Contains(s, `@('Chromium','(it''s''); Remove-Item C:\ -Recurse; (''')`) {
		t.Fatalf("title parts not safely single-quoted:\n%s", s[strings.LastIndex(s, "if ("):])
	}
}
