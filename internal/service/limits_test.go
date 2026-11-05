package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
)

func TestKeywordLimits(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	many := make([]string, MaxTermsPerList+1)
	for i := range many {
		many[i] = strings.Repeat("x", i%7+1) + string(rune('a'+i%26))
	}
	if _, err := s.SetKeywords(ctx, keyword.Sets{"search_queries": many}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%d terms = %v, want ErrInvalid", len(many), err)
	}
	if _, err := s.SetKeywords(ctx, keyword.Sets{"search_queries": {strings.Repeat("é", MaxTermChars+1)}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long term = %v, want ErrInvalid", err)
	}
	if _, err := s.SetKeywords(ctx, keyword.Sets{"search_queries": {strings.Repeat("é", MaxTermChars), "n8n"}}); err != nil {
		t.Fatalf("terms within the limits: %v", err)
	}
}

func TestMaxJobs(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.SetMaxJobs(2)
	for _, title := range []string{"One", "Two"} {
		if _, err := s.AddJob(ctx, NewJob{Title: title, Company: "Duck Co"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddJob(ctx, NewJob{Title: "Three", Company: "Duck Co"}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "full") {
		t.Fatalf("third job on a 2-job board = %v", err)
	}
	s.SetMaxJobs(0)
	if _, err := s.AddJob(ctx, NewJob{Title: "Three", Company: "Duck Co"}); err != nil {
		t.Fatalf("no cap: %v", err)
	}
}

func TestCVVersionCap(t *testing.T) {
	s, id := docSvc(t)
	ctx := context.Background()
	cv := "# Alice\n\nGo developer with five years of building backend services."
	for i := range maxCVVersions {
		if _, err := s.SubmitCV(ctx, id, cv); err != nil {
			t.Fatalf("version %d: %v", i+1, err)
		}
	}
	if _, err := s.SubmitCV(ctx, id, cv); !errors.Is(err, ErrInvalid) {
		t.Fatalf("version %d = %v, want ErrInvalid", maxCVVersions+1, err)
	}
}
