package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/rating"
)

func TestRatingConfigDefaultWhenAbsent(t *testing.T) {
	s := open(t)
	got, err := s.GetRatingConfig(context.Background())
	if err != nil {
		t.Fatalf("GetRatingConfig: %v", err)
	}
	want := rating.Normalize(rating.Config{})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fresh config = %+v, want %+v", got, want)
	}
}

func TestRatingConfigRoundTrip(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	in := rating.Config{ABEnabled: true, Models: []string{"a", "b", "c"}, DefaultModel: "b"}

	saved, err := s.SetRatingConfig(ctx, in)
	if err != nil {
		t.Fatalf("SetRatingConfig: %v", err)
	}
	if !reflect.DeepEqual(saved, in) {
		t.Errorf("saved = %+v, want %+v", saved, in)
	}
	got, _ := s.GetRatingConfig(ctx)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("reread = %+v, want %+v", got, in)
	}

	// Upsert overwrites the single row.
	next := rating.Config{ABEnabled: false, Models: []string{"x"}, DefaultModel: "x"}
	if _, err := s.SetRatingConfig(ctx, next); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetRatingConfig(ctx)
	if !reflect.DeepEqual(got, next) {
		t.Errorf("after upsert = %+v, want %+v", got, next)
	}
}

func TestSetRatingConfigRejectsInvalid(t *testing.T) {
	s := open(t)
	_, err := s.SetRatingConfig(context.Background(),
		rating.Config{Models: []string{"a", "b"}, DefaultModel: "zzz"})
	if err == nil {
		t.Error("expected validation error for default not in set")
	}
}
