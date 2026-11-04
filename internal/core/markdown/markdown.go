// Package markdown renders the small Markdown subset braveduck's agents are
// asked to write CVs in (headings, bullets, paragraphs, **bold**, *italic*,
// [links](https://…)) to safe HTML. Everything is escaped first; only http(s)
// and mailto links survive, so agent output can't inject markup or script.
package markdown

import (
	"html"
	"regexp"
	"strings"
)

var (
	boldRE   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRE = regexp.MustCompile(`(^|[^*])\*([^*\s][^*]*)\*`)
	linkRE   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

func inline(s string) string {
	s = html.EscapeString(s)
	s = linkRE.ReplaceAllStringFunc(s, func(m string) string {
		p := linkRE.FindStringSubmatch(m)
		u := html.UnescapeString(p[2])
		lu := strings.ToLower(u)
		if !strings.HasPrefix(lu, "https://") && !strings.HasPrefix(lu, "http://") && !strings.HasPrefix(lu, "mailto:") {
			return p[1] // drop the link, keep the text
		}
		return `<a href="` + html.EscapeString(u) + `">` + p[1] + `</a>`
	})
	s = boldRE.ReplaceAllString(s, "<strong>$1</strong>")
	s = italicRE.ReplaceAllString(s, "$1<em>$2</em>")
	return s
}

// ToHTML renders md as an HTML fragment.
func ToHTML(md string) string {
	var b strings.Builder
	inList := false
	var para []string
	flush := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			flush()
			closeList()
		case strings.HasPrefix(line, "#"):
			flush()
			closeList()
			level := len(line) - len(strings.TrimLeft(line, "#"))
			if level > 3 {
				level = 3
			}
			tag := []string{"", "h1", "h2", "h3"}[level]
			b.WriteString("<" + tag + ">" + inline(strings.TrimSpace(strings.TrimLeft(line, "#"))) + "</" + tag + ">\n")
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "• "):
			flush()
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("<li>" + inline(strings.TrimSpace(line[strings.Index(line, " ")+1:])) + "</li>\n")
		default:
			closeList()
			para = append(para, line)
		}
	}
	flush()
	closeList()
	return b.String()
}

// Page wraps a rendered CV in a printable A4 page.
func Page(title, body string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title>
<style>
@page { size: A4; margin: 16mm 16mm; }
body { font: 10.5pt/1.45 "Segoe UI", "Noto Sans", Arial, sans-serif; color: #1a1a1a; }
h1 { font-size: 20pt; margin: 0 0 4pt; }
h2 { font-size: 12pt; text-transform: uppercase; letter-spacing: .04em; border-bottom: 1px solid #bbb; padding-bottom: 2pt; margin: 14pt 0 6pt; }
h3 { font-size: 11pt; margin: 8pt 0 2pt; }
p { margin: 3pt 0; } ul { margin: 2pt 0 4pt 16pt; padding: 0; } li { margin: 1pt 0; }
a { color: #0b57d0; text-decoration: none; }
</style></head><body>` + body + `</body></html>`
}
