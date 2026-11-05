package cvfile

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// docx builds a minimal Word file around body (the inside of <w:body>).
func docx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + `</w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func read(t *testing.T, name string, data []byte) (string, error) {
	t.Helper()
	k, err := Detect(name, data)
	if err != nil {
		return "", err
	}
	return Text(context.Background(), k, data)
}

func TestTextFromEachFormat(t *testing.T) {
	pdf, err := os.ReadFile("testdata/cv-vi.pdf") // printed by Chromium, like most CV builders
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want []string
	}{
		{"cv.pdf", pdf, []string{
			"Nguyễn Văn Khang",
			"Chuyên viên Marketing AI — 3 năm kinh nghiệm tự động hoá với n8n.",
			"xây dựng 40 workflow n8n", // the PDF's "ﬂ" ligature comes back as "fl"
			"khang@example.com",
		}},
		{"CV.DOCX", docx(t, `<w:p><w:r><w:t>Nguyễn Văn Khang</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t xml:space="preserve">Marketing AI, </w:t></w:r><w:r><w:t>3 năm kinh nghiệm</w:t></w:r><w:r><w:br/><w:t>Công ty TNHH Vịt Vàng</w:t></w:r></w:p>`),
			[]string{"Nguyễn Văn Khang\nMarketing AI, 3 năm kinh nghiệm\nCông ty TNHH Vịt Vàng"}},
		{"cv.html", []byte(`<html><head><title>x</title><style>h1{color:red}</style></head><body><h1>Nguyễn Văn Khang</h1>` +
			`<script>alert(1)</script><p>Marketing AI &amp; n8n, 3 năm kinh nghiệm.</p><ul><li>Công ty TNHH Vịt Vàng</li></ul></body></html>`),
			[]string{"Nguyễn Văn Khang\n", "\nMarketing AI & n8n, 3 năm kinh nghiệm.\n", "\nCông ty TNHH Vịt Vàng"}},
		{"cv.md", []byte("# Nguyễn Văn Khang\r\n\r\n\r\n\r\n- Marketing AI, 3 năm kinh nghiệm với n8n   \r\n"),
			[]string{"# Nguyễn Văn Khang\n\n- Marketing AI, 3 năm kinh nghiệm với n8n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := read(t, tc.name, tc.data)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("text is missing %q:\n%s", w, got)
				}
			}
			if strings.Contains(got, "alert") || strings.Contains(got, "color:red") {
				t.Errorf("script/style leaked into the text:\n%s", got)
			}
		})
	}
}

func TestRefusesWhatItCantRead(t *testing.T) {
	blank, err := os.ReadFile("testdata/no-text.pdf") // like a scanned CV: a page with no text layer
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"cv.txt", []byte("plain text CVs aren't on the list"), ErrUnsupported},
		{"cv.doc", []byte("\xd0\xcf\x11\xe0 old Word"), ErrUnsupported},
		{"cv.pdf", []byte("<html>renamed, not a PDF</html>"), ErrUnsupported},
		{"cv.md", []byte("\x89PNG\r\n\x1a\n\x00\x00"), ErrUnsupported},
		{"cv.docx", []byte("PK\x03\x04 broken zip"), nil}, // any error
		{"cv.pdf", []byte("%PDF-1.7\n garbage that isn't a PDF"), nil},
		{"cv.pdf", blank, ErrNoText},
		{"cv.md", []byte("# Me"), ErrNoText},
	} {
		_, err := read(t, tc.name, tc.data)
		if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s %q: err = %v, want %v", tc.name, trim(tc.data), err, tc.want)
		}
	}
}

func trim(b []byte) string {
	if len(b) > 24 {
		b = b[:24]
	}
	return string(b)
}
