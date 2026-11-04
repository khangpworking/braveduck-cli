package markdown

import (
	"strings"
	"testing"
)

func TestToHTML(t *testing.T) {
	got := ToHTML("# Nguyễn Văn A\n\n## Kinh nghiệm\n- **Duck Co** — built *n8n* flows\n- [Portfolio](https://a.dev)\n\nLine one\nline two")
	for _, want := range []string{
		"<h1>Nguyễn Văn A</h1>", "<h2>Kinh nghiệm</h2>", "<ul>", "<li><strong>Duck Co</strong> — built <em>n8n</em> flows</li>",
		`<a href="https://a.dev">Portfolio</a>`, "<p>Line one line two</p>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToHTMLIsSafe(t *testing.T) {
	got := ToHTML(`# <script>alert(1)</script>
- [click](javascript:alert(1)) and [x](data:text/html,<b>)
<img src=x onerror=alert(1)>`)
	for _, bad := range []string{"<script", "<img", `href="javascript`, `href="data`} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe output %q in:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "click") {
		t.Fatal("link text dropped along with the unsafe link")
	}
}
