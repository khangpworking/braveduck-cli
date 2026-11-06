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

// On a shared server built-in crawls run at most hourly per user.
func TestHostedCrawlInterval(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if _, err := s.SetCrawlSchedule(ctx, "vietnamworks", 30); err != nil {
		t.Fatalf("own computer, every 30 min: %v", err)
	}
	s.SetHosted(HostedInfo{User: "alice"})
	if _, err := s.SetCrawlSchedule(ctx, "vietnamworks", 30); !errors.Is(err, ErrInvalid) {
		t.Fatalf("shared server, every 30 min: %v, want ErrInvalid", err)
	}
	if _, err := s.SetCrawlSchedule(ctx, "vietnamworks", HostedMinEveryMinutes); err != nil {
		t.Fatalf("shared server, hourly: %v", err)
	}
	// The board offers only what the server accepts.
	scheds, err := s.CrawlSchedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range scheds {
		if sc.Builtin && sc.MinEveryMinutes != HostedMinEveryMinutes {
			t.Errorf("%s: minimum = %d, want %d", sc.Source, sc.MinEveryMinutes, HostedMinEveryMinutes)
		}
	}
}
