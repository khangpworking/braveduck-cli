package store

import (
	"context"
	"testing"
)

func TestKeywordAddListRemove(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	// Fresh DB → every list empty.
	terms, err := s.ListKeywords(ctx, "negative_title")
	if err != nil {
		t.Fatalf("ListKeywords: %v", err)
	}
	if len(terms) != 0 {
		t.Errorf("fresh list should be empty, got %v", terms)
	}

	// Add with a blank and a dup; only the two distinct terms land.
	n, err := s.AddKeywords(ctx, "negative_title", []string{"intern", "", "clinic", "intern"})
	if err != nil {
		t.Fatalf("AddKeywords: %v", err)
	}
	if n != 2 {
		t.Errorf("added = %d, want 2", n)
	}
	terms, _ = s.ListKeywords(ctx, "negative_title")
	if len(terms) != 2 || terms[0] != "clinic" || terms[1] != "intern" { // sorted
		t.Errorf("stored terms = %v, want [clinic intern]", terms)
	}

	// Case-insensitive removal.
	rm, err := s.RemoveKeywords(ctx, "negative_title", []string{"INTERN"})
	if err != nil {
		t.Fatalf("RemoveKeywords: %v", err)
	}
	if rm != 1 {
		t.Errorf("removed = %d, want 1", rm)
	}
	terms, _ = s.ListKeywords(ctx, "negative_title")
	if len(terms) != 1 || terms[0] != "clinic" {
		t.Errorf("after remove = %v, want [clinic]", terms)
	}
}

func TestKeywordUnknownList(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if _, err := s.ListKeywords(ctx, "nonsense"); err == nil {
		t.Error("expected error for unknown list key")
	}
	if _, err := s.AddKeywords(ctx, "nonsense", []string{"x"}); err == nil {
		t.Error("expected error adding to unknown list")
	}
}

func TestAllKeywordsShape(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.AddKeywords(ctx, "positive_title", []string{"marketing"})
	s.AddKeywords(ctx, "negative_company", []string{"nha khoa"})

	sets, err := s.AllKeywords(ctx)
	if err != nil {
		t.Fatalf("AllKeywords: %v", err)
	}
	// Every managed key present (even empty ones), populated ones correct.
	if len(sets["positive_title"]) != 1 || sets["positive_title"][0] != "marketing" {
		t.Errorf("positive_title = %v", sets["positive_title"])
	}
	if _, ok := sets["search_queries"]; !ok {
		t.Error("AllKeywords should include every managed list key")
	}
}
