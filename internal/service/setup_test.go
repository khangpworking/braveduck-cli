package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSetupWalksTheSteps(t *testing.T) {
	s, profile := newRatingSvc(t)
	ctx := context.Background()

	st, err := s.Setup(ctx)
	if err != nil || st.Done || st.CV || st.AboutMe || st.Wants || st.SearchQueries != 0 {
		t.Fatalf("fresh setup = %+v %v, want nothing done", st, err)
	}

	// The CV fills "About me" only; what the user wants is a separate step.
	s.UploadCV(ctx, "cv.md", []byte(cvMD), false)
	if st, _ := s.Setup(ctx); !st.CV || !st.AboutMe || st.Wants || st.Done {
		t.Fatalf("after CV = %+v", st)
	}

	p, _ := s.Profile(ctx)
	text := strings.Replace(p.Text, "- Minimum I'll accept:", "- Minimum I'll accept: 20 triệu/tháng net", 1)
	if _, err := s.SaveProfile(ctx, text); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Setup(ctx); !st.Wants || st.Done {
		t.Fatalf("after wants = %+v, want Wants but not Done (no search terms)", st)
	}

	s.st.AddKeywords(ctx, "search_queries", []string{"n8n"})
	if st, _ := s.Setup(ctx); !st.Done || st.SearchQueries != 1 || st.Location || st.TopCV {
		t.Fatalf("after search terms = %+v, want Done (location and TopCV are optional)", st)
	}

	if fi, _ := os.Stat(profile); fi.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode %v, want private", fi.Mode().Perm())
	}
}

func TestSaveProfile(t *testing.T) {
	s, profile := newRatingSvc(t)
	ctx := context.Background()
	p, err := s.SaveProfile(ctx, "# About me\r\nData analyst, five years of SQL")
	if err != nil || !p.Filled {
		t.Fatalf("save = %+v %v", p, err)
	}
	if b, _ := os.ReadFile(profile); string(b) != "# About me\nData analyst, five years of SQL\n" {
		t.Fatalf("stored %q, want LF line endings and a final newline", b)
	}
	if _, err := s.SaveProfile(ctx, strings.Repeat("x", maxProfileChars+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too long = %v, want ErrInvalid", err)
	}
}
