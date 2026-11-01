package store

import (
	"context"
	"testing"
)

func TestSessionsCRUD(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	if v, _ := s.SchemaVersion(ctx); v != len(migrations) || v < 3 {
		t.Fatalf("schema v%d, want v%d (>=3)", v, len(migrations))
	}
	if _, err := s.GetSession(ctx, "topcv"); !IsNotFound(err) {
		t.Fatalf("missing session = %v", err)
	}

	ps := PlatformSession{Platform: "topcv", Enc: []byte{1, 2, 3}, CreatedAt: "2026-11-01T00:00:00Z",
		FPSeed: "12345", Status: SessionConnected, AccountHint: "ng…@gmail.com"}
	if err := s.SaveSession(ctx, ps); err != nil {
		t.Fatal(err)
	}
	ps.Enc, ps.FPSeed = []byte{9}, "777" // re-login replaces
	if err := s.SaveSession(ctx, ps); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, "topcv")
	if err != nil || string(got.Enc) != "\x09" || got.FPSeed != "777" || got.AccountHint != "ng…@gmail.com" {
		t.Fatalf("got %+v, %v", got, err)
	}

	if err := s.SetSessionStatus(ctx, "topcv", SessionExpired, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSession(ctx, "topcv"); got.Status != SessionExpired || got.CheckedAt == "" {
		t.Fatalf("status not recorded: %+v", got)
	}
	if err := s.SetSessionStatus(ctx, "nope", SessionExpired, now); !IsNotFound(err) {
		t.Fatalf("status on missing = %v", err)
	}

	list, _ := s.ListSessions(ctx)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if del, err := s.DeleteSession(ctx, "topcv"); !del || err != nil {
		t.Fatalf("delete = %v, %v", del, err)
	}
	if del, err := s.DeleteSession(ctx, "topcv"); del || err != nil {
		t.Fatalf("second delete = %v, %v", del, err)
	}
}
