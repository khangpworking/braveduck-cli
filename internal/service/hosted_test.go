package service

import (
	"context"
	"errors"
	"testing"
)

// On a shared server the features that open a window on "the user's
// computer" refuse with ErrHosted (a 400 on the board) instead of opening it
// on the server, and the board learns who is logged in.
func TestHostedRefusesWindowsAndReportsUser(t *testing.T) {
	s, fb := newPlatformSvc(t)
	s.SetHosted(HostedInfo{User: "alice", LogoutURL: "https://auth.example.com/logout"})
	ctx := context.Background()

	if _, err := s.Connect(ctx, "topcv", LoginHooks{}); !errors.Is(err, ErrHosted) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("Connect = %v, want ErrHosted", err)
	}
	if _, err := s.StartConnect("topcv"); !errors.Is(err, ErrHosted) {
		t.Fatalf("StartConnect = %v, want ErrHosted", err)
	}
	if fb.logins.Load() != 0 {
		t.Fatal("a login window was opened")
	}
	j, err := s.AddJob(ctx, NewJob{Title: "Dev", URL: "https://www.topcv.vn/viec-lam/dev/1.html"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartAssistApply(j.ID); !errors.Is(err, ErrHosted) {
		t.Fatalf("StartAssistApply = %v, want ErrHosted", err)
	}

	st, err := s.Setup(ctx)
	if err != nil || st.Hosted == nil || st.Hosted.User != "alice" || st.Hosted.LogoutURL == "" {
		t.Fatalf("Setup = %+v, %v", st, err)
	}
}
