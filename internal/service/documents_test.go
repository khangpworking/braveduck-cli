package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func docSvc(t *testing.T) (*Service, int64) {
	t.Helper()
	s, p := newRatingSvc(t)
	os.MkdirAll(strings.TrimSuffix(p, "/profile.md"), 0o700)
	os.WriteFile(p, []byte("Backend engineer, 5 years Go and Postgres."), 0o600)
	j, _ := s.AddJob(context.Background(), NewJob{Title: "Go Engineer", Company: "Duck Co", JD: "Go, Postgres. Gửi CV về hr@duck.vn"})
	return s, j.ID
}

func TestCVVersionsArePrivateFiles(t *testing.T) {
	s, id := docSvc(t)
	ctx := context.Background()

	b, err := s.CVBrief(ctx, id)
	if err != nil || b.Kind != "cv" || !strings.Contains(b.Profile, "Backend") || b.Previous != "" || !strings.Contains(b.Instructions, "never invent") {
		t.Fatalf("brief = %+v, %v", b, err)
	}
	if _, err := s.SubmitCV(ctx, id, "too short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short CV = %v", err)
	}
	cv := "# Nguyễn Văn A\n\n## Experience\n- Go and Postgres at Duck Co, five years of backend work"
	j, err := s.SubmitCV(ctx, id, cv)
	if err != nil || j.CVVersion != 1 || j.CVStatus != "ready" {
		t.Fatalf("submit = %+v, %v", j, err)
	}
	if fi, _ := os.Stat(j.CVPath); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatal("CV file missing or not 0600")
	}
	j, _ = s.SubmitCV(ctx, id, cv+"\n- and more")
	if j.CVVersion != 2 {
		t.Fatalf("second version = %d", j.CVVersion)
	}
	if b, _ := s.CVBrief(ctx, id); !strings.Contains(b.Previous, "and more") {
		t.Fatal("re-tailor brief doesn't include the current version")
	}
	if md, _, err := s.CV(ctx, id); err != nil || !strings.Contains(md, "and more") {
		t.Fatalf("CV = %q, %v", md, err)
	}
}

func TestDraftLifecycle(t *testing.T) {
	s, id := docSvc(t)
	ctx := context.Background()

	b, _ := s.DraftBrief(ctx, id, "stress Postgres")
	if b.ContactEmail != "hr@duck.vn" || b.Notes != "stress Postgres" {
		t.Fatalf("brief = %+v", b)
	}
	if j, _ := s.GetJob(ctx, id); j.DraftNotes != "stress Postgres" {
		t.Fatal("notes not kept for next time")
	}
	for _, bad := range []Draft{
		{Subject: "", Body: "long enough body for the test"},
		{Subject: "Hi", Body: "short"},
		{To: "not an email", Subject: "Hi", Body: "long enough body for the test"},
	} {
		if _, err := s.SubmitDraft(ctx, id, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("SubmitDraft(%+v) = %v", bad, err)
		}
	}
	if _, err := s.MarkDraftSent(ctx, id); !errors.Is(err, ErrInvalid) {
		t.Fatal("marked sent with no draft")
	}

	j, err := s.SubmitDraft(ctx, id, Draft{Subject: "Go Engineer — application", Body: "Hello, I'd like to apply for this role."})
	if err != nil || j.DraftStatus != DraftReady || !strings.Contains(j.DraftEmail, `"to":"hr@duck.vn"`) {
		t.Fatalf("draft = %q / %q, %v", j.DraftStatus, j.DraftEmail, err)
	}
	j, _ = s.MarkDraftSent(ctx, id)
	if j.DraftStatus != DraftSent || j.Status != "applied" || j.ApplyState != "sent" {
		t.Fatalf("after sent = %q %q %q", j.DraftStatus, j.Status, j.ApplyState)
	}
	if j, _ = s.RejectDraft(ctx, id); j.DraftEmail != "" || j.DraftStatus != DraftRejected {
		t.Fatalf("after reject = %+v", j)
	}
}

func TestDocBriefsNeedAProfile(t *testing.T) {
	s, _ := newRatingSvc(t)
	j, _ := s.AddJob(context.Background(), NewJob{Title: "X"})
	if _, err := s.CVBrief(context.Background(), j.ID); !errors.Is(err, ErrProfileEmpty) {
		t.Fatalf("empty profile = %v", err)
	}
}
