// Package cvfile reads the text out of a CV the user uploads: PDF, Word
// (.docx), Markdown or HTML. The text fills the profile that jobs are rated
// and tailored against; the file itself is kept for applying. Pure (bytes
// in, text out), no network.
package cvfile

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/text/unicode/norm"
)

// MaxBytes is the largest CV accepted (TopCV's own upload limit).
const MaxBytes = 5 << 20

// Kind is a supported CV format.
type Kind string

const (
	PDF      Kind = "pdf"
	DOCX     Kind = "docx"
	Markdown Kind = "md"
	HTML     Kind = "html"
)

var (
	// ErrUnsupported is a file that isn't one of the supported formats.
	ErrUnsupported = errors.New("upload a PDF, Word (.docx), Markdown (.md) or HTML CV")
	// ErrNoText is a CV with no readable text, e.g. a scanned PDF.
	ErrNoText = errors.New("no text could be read from this CV (a scanned or image-only PDF has none) — upload it as .docx or .md instead")
)

var extKinds = map[string]Kind{".pdf": PDF, ".docx": DOCX, ".md": Markdown, ".markdown": Markdown, ".html": HTML, ".htm": HTML}

// Detect returns the file's kind from its name, checked against its content
// so a renamed file of another type is refused.
func Detect(name string, data []byte) (Kind, error) {
	k, ok := extKinds[strings.ToLower(filepath.Ext(name))]
	if !ok {
		return "", ErrUnsupported
	}
	switch k {
	case PDF:
		ok = bytes.HasPrefix(data, []byte("%PDF-"))
	case DOCX:
		ok = bytes.HasPrefix(data, []byte("PK\x03\x04"))
	default:
		ok = utf8.Valid(data) && !bytes.ContainsRune(data, 0)
	}
	if !ok {
		return "", fmt.Errorf("%w (%s doesn't look like a %s file)", ErrUnsupported, filepath.Base(name), k)
	}
	return k, nil
}

// Text extracts the CV's text, tidied: trimmed lines, at most one blank line
// in a row. ErrNoText when there's (almost) none.
func Text(k Kind, data []byte) (string, error) {
	var raw string
	var err error
	switch k {
	case PDF:
		raw, err = pdfText(data)
	case DOCX:
		raw, err = docxText(data)
	case HTML:
		raw = htmlText(string(data))
	case Markdown:
		raw = string(data)
	default:
		return "", ErrUnsupported
	}
	if err != nil {
		return "", err
	}
	// NFKC folds ligatures PDFs use ("ﬂ" → "fl") and composes accents.
	text := tidy(norm.NFKC.String(raw))
	if len([]rune(text)) < 50 {
		return "", ErrNoText
	}
	return text, nil
}

// pdfText reads a PDF's text layer. (Reading by rows splits words where the
// PDF kerns them, so the plain-text reader is used.) The
// parser panics on some malformed files; that's a bad upload, not a crash.
func pdfText(data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("this PDF couldn't be read: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("this PDF couldn't be read: %w", err)
	}
	t, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("this PDF couldn't be read: %w", err)
	}
	b, err := io.ReadAll(t)
	return string(b), err
}

// maxDocXML caps the unpacked document.xml, so a zip bomb can't fill memory.
const maxDocXML = 20 << 20

// docxText reads word/document.xml: text runs, with paragraphs, line breaks
// and tabs kept.
func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("this .docx couldn't be read: %w", err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
		}
	}
	if doc == nil {
		return "", fmt.Errorf("%w (no Word document inside this .docx)", ErrUnsupported)
	}
	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, maxDocXML))
	var b strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("this .docx couldn't be read: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br", "cr":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return b.String(), nil
}

var (
	htmlDropRE  = regexp.MustCompile(`(?is)<(script|style|head|template|noscript)\b.*?</(script|style|head|template|noscript)\s*>|<!--.*?-->`)
	htmlBlockRE = regexp.MustCompile(`(?i)<(br|/?(p|div|li|ul|ol|tr|table|section|article|header|footer|h[1-6]|blockquote|pre))\b[^>]*>`)
	htmlTagRE   = regexp.MustCompile(`(?s)<[^>]*>`)
)

// htmlText turns an HTML CV into plain text: scripts and styles dropped,
// block elements on their own lines, entities decoded.
func htmlText(s string) string {
	s = htmlDropRE.ReplaceAllString(s, " ")
	s = htmlBlockRE.ReplaceAllString(s, "\n")
	s = htmlTagRE.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

var spacesRE = regexp.MustCompile(`[ \t\x{00a0}]+`)

// tidy trims each line, collapses runs of spaces and blank lines.
func tidy(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	blank := true // drops leading blank lines
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(spacesRE.ReplaceAllString(line, " "))
		if line == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
