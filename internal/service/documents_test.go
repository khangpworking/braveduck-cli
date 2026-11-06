package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/store"
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

func TestConcurrentCVVersionsPreserveEverySubmission(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var services []*Service
	for range 2 {
		st, err := store.Open(filepath.Join(dir, "board.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		s := New(st)
		s.SetDataDir(dir)
		services = append(services, s)
	}
	j, err := services[0].AddJob(ctx, NewJob{Title: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	type submission struct {
		job      jobs.Job
		markdown string
		err      error
	}
	const writers = maxCVVersions + 10
	start := make(chan struct{})
	results := make(chan submission, writers)
	for i := range writers {
		go func() {
			<-start
			markdown := fmt.Sprintf("# Candidate\n\nSubmission %d: five years of building Go backend services.", i)
			got, err := services[i%len(services)].SubmitCV(ctx, j.ID, markdown)
			results <- submission{got, markdown, err}
		}()
	}
	close(start)
	var accepted []submission
	versions := map[int]bool{}
	for range writers {
		r := <-results
		if r.err == nil {
			accepted = append(accepted, r)
			versions[r.job.CVVersion] = true
		} else if !errors.Is(r.err, ErrInvalid) || !strings.Contains(r.err.Error(), "CV versions") {
			t.Errorf("submit: %v", r.err)
		}
	}
	if len(accepted) != maxCVVersions || len(versions) != maxCVVersions {
		t.Fatalf("accepted %d submissions with %d distinct versions; want %d of each", len(accepted), len(versions), maxCVVersions)
	}
	for _, r := range accepted {
		data, err := os.ReadFile(r.job.CVPath)
		if err != nil || string(data) != r.markdown+"\n" {
			t.Errorf("version %d lost its submitted content: %q, %v", r.job.CVVersion, data, err)
		}
	}
	current, err := services[0].GetJob(ctx, j.ID)
	if err != nil || current.CVVersion != maxCVVersions || current.CVStatus != "ready" {
		t.Fatalf("current CV = %+v, %v", current, err)
	}
}

func TestFailedCVWritePreservesPublishedVersion(t *testing.T) {
	s, id := docSvc(t)
	ctx := context.Background()
	markdown := "# Candidate\n\nFive years of building Go backend services and databases."
	first, err := s.SubmitCV(ctx, id, markdown)
	if err != nil {
		t.Fatal(err)
	}
	// The next version's filename cannot be written as a regular file.
	blocked := filepath.Join(filepath.Dir(first.CVPath), fmt.Sprintf("job-%d-v2.md", id))
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCV(ctx, id, markdown+"\nMore experience."); err == nil {
		t.Fatal("submit succeeded despite the blocked output file")
	}
	text, current, err := s.CV(ctx, id)
	if err != nil || text != markdown+"\n" || current.CVVersion != 1 || current.CVPath != first.CVPath || current.CVStatus != "ready" {
		t.Fatalf("failed write changed published CV: %+v, %q, %v", current, text, err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if next, err := s.SubmitCV(ctx, id, markdown+"\nMore experience."); err != nil || next.CVVersion != 2 {
		t.Fatalf("retry = version %d, %v; want version 2", next.CVVersion, err)
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

// Deleting a job deletes its tailored CVs, and only its own.
func TestDeleteJobRemovesItsCVs(t *testing.T) {
	s, id := docSvc(t)
	ctx := context.Background()
	other, _ := s.AddJob(ctx, NewJob{Title: "Other", Company: "Duck Co"})
	cv := "# Alice\n\nGo developer with five years of building backend services."
	for _, job := range []int64{id, id, other.ID} {
		if _, err := s.SubmitCV(ctx, job, cv); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(filepath.Dir(s.profilePath), "cvs")
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("job-%d-v1.pdf", id)), []byte("%PDF"), 0o600)
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("job-%d1-v1.md", id)), []byte("job #"+fmt.Sprint(id)+"1's"), 0o600)
	if _, err := s.DeleteJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	var names []string
	for _, f := range left {
		names = append(names, filepath.Base(f))
	}
	want := []string{fmt.Sprintf("job-%d-v1.md", other.ID), fmt.Sprintf("job-%d1-v1.md", id)}
	slices.Sort(want)
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Fatalf("left in cvs/: %v, want %v", names, want)
	}
}
