package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cvMD = "# Nguyễn Văn Khang\n\n## Kinh nghiệm\n- Công ty TNHH Vịt Vàng: 40 workflow n8n, giảm 60% thời gian báo cáo\n"

func TestUploadCVFillsTheProfile(t *testing.T) {
	s, profile := newRatingSvc(t)
	ctx := context.Background()
	if _, ok, _ := s.BaseCV(ctx); ok {
		t.Fatal("a base CV before any upload")
	}

	cv, err := s.UploadCV(ctx, `C:\Users\me\Downloads\CV Khang (2026).md`, []byte(cvMD), false)
	if err != nil {
		t.Fatal(err)
	}
	if cv.Name != "CV Khang (2026).md" || cv.Kind != "md" || !cv.ProfileFilled || filepath.Dir(cv.Path) != filepath.Join(filepath.Dir(profile), "cv") {
		t.Fatalf("cv = %+v", cv)
	}
	if fi, err := os.Stat(cv.Path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stored file: %v %v, want private", fi, err)
	}

	b, _ := os.ReadFile(profile)
	text := string(b)
	about := text[strings.Index(text, "# About me"):strings.Index(text, "# What I'm looking for")]
	// The CV sits under "About me" with its headings moved inside the
	// section; the guidance comment and the other sections are untouched.
	for _, want := range []string{"### Nguyễn Văn Khang", "#### Kinh nghiệm", "40 workflow n8n", "<!-- A short summary"} {
		if !strings.Contains(about, want) {
			t.Errorf("About me is missing %q:\n%s", want, about)
		}
	}
	if !strings.Contains(text, "# Compensation") || !strings.Contains(text, "- Minimum I'll accept:") {
		t.Fatalf("other sections changed:\n%s", text)
	}
	if got, ok, _ := s.BaseCV(ctx); !ok || got.Path != cv.Path || !got.ProfileFilled {
		t.Fatalf("BaseCV() = %+v %v", got, ok)
	}
}

func TestUploadCVAsksBeforeReplacingAboutMe(t *testing.T) {
	s, profile := newRatingSvc(t)
	ctx := context.Background()
	first, _ := s.UploadCV(ctx, "old.md", []byte(cvMD), false)
	before, _ := os.ReadFile(profile)

	newer := []byte(strings.ReplaceAll(cvMD, "Vịt Vàng", "Duck Pay JSC"))
	if _, err := s.UploadCV(ctx, "new.md", newer, false); !errors.Is(err, ErrAboutMeFilled) {
		t.Fatalf("second upload = %v, want ErrAboutMeFilled", err)
	}
	if after, _ := os.ReadFile(profile); string(after) != string(before) {
		t.Fatal("profile changed without replace")
	}
	if cv, _, _ := s.BaseCV(ctx); cv.Path != first.Path {
		t.Fatal("base CV changed without replace")
	}

	cv, err := s.UploadCV(ctx, "new.md", newer, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(profile)
	if !strings.Contains(string(b), "Duck Pay JSC") || strings.Contains(string(b), "Vịt Vàng") || strings.Count(string(b), "# About me") != 1 {
		t.Fatalf("replace didn't swap the text:\n%s", b)
	}
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) || cv.Path == first.Path {
		t.Fatal("old CV file kept after replace")
	}
}

func TestUploadCVRefusesUnreadableFiles(t *testing.T) {
	s, profile := newRatingSvc(t)
	ctx := context.Background()
	for name, data := range map[string][]byte{
		"cv.txt":  []byte(cvMD),
		"cv.pdf":  []byte("not a pdf at all, just text renamed"),
		"cv.md":   []byte("# Me"),
		"big.pdf": append([]byte("%PDF-"), make([]byte, 5<<20)...),
	} {
		if _, err := s.UploadCV(ctx, name, data, true); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if _, ok, _ := s.BaseCV(ctx); ok {
		t.Fatal("a refused upload was stored")
	}
	if p, _ := s.Profile(ctx); p.Filled {
		t.Fatalf("a refused upload changed %s", profile)
	}
}

// A profile the user rewrote without an "About me" heading gets the
// section added at the top.
func TestWithAboutMeAddsMissingSection(t *testing.T) {
	got, had := withAboutMe("# Wants\n- remote only\n", "Data analyst, 5 years")
	if had || !strings.HasPrefix(got, "# About me\n\nData analyst, 5 years\n\n# Wants\n- remote only") {
		t.Fatalf("got %v %q", had, got)
	}
}

func TestApplyUsesBaseCVWithoutATailoredOne(t *testing.T) {
	s, _ := applySvc(t)
	ctx := context.Background()
	j, _ := s.AddJob(ctx, NewJob{Title: "Untailored", URL: "https://www.topcv.vn/viec-lam/x/2200009.html"})

	s.UploadCV(ctx, "cv.md", []byte(cvMD), true)
	prep, err := s.PrepareApply(ctx, j.ID)
	if err != nil || !strings.HasSuffix(prep.CVPDF, "/cv/cv.pdf") {
		t.Fatalf("markdown base CV: %+v %v, want it printed to cv/cv.pdf", prep, err)
	}
	if b, _ := os.ReadFile(prep.CVPDF); string(b) != "%PDF fake" {
		t.Fatalf("printed PDF = %q", b)
	}

	// A PDF (or .docx) CV is offered as uploaded.
	pdf, err := os.ReadFile("../core/cvfile/testdata/cv-vi.pdf")
	if err != nil {
		t.Fatal(err)
	}
	cv, err := s.UploadCV(ctx, "Khang CV.pdf", pdf, true)
	if err != nil {
		t.Fatal(err)
	}
	if prep, err := s.PrepareApply(ctx, j.ID); err != nil || prep.CVPDF != cv.Path {
		t.Fatalf("PDF base CV: %+v %v, want %s", prep, err, cv.Path)
	}

	// A tailored CV still wins.
	s.SubmitCV(ctx, j.ID, "# A\n\n## Experience\n- tailored for this job, long enough to be kept")
	if prep, _ := s.PrepareApply(ctx, j.ID); !strings.Contains(prep.CVPDF, "/cvs/job-") {
		t.Fatalf("tailored CV not used: %+v", prep)
	}
}
