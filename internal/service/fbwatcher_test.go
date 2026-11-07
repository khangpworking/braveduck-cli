package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/fbwatcher"
)

// fakeFB is fb-watcher: it answers with post or err, and records calls.
type fakeFB struct {
	post   fbwatcher.Post
	group  fbwatcher.Group
	err    error
	called chan string
}

func (f *fakeFB) FetchPost(_ context.Context, link string) (fbwatcher.Post, error) {
	return f.post, f.err
}

func (f *fakeFB) CheckGroup(_ context.Context, link string) (fbwatcher.Group, error) {
	defer func() { f.called <- link }()
	return f.group, f.err
}

const fbPost = "https://www.facebook.com/groups/398/posts/77/"

// With fb-watcher connected, a Facebook post link on its own becomes a job
// with the post's text; without it, a bare-link card as before.
func TestAddFacebookPostReadByFBWatcher(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	j, err := s.AddFromContent(ctx, fbPost)
	if err != nil || j.FullJD != "" {
		t.Fatalf("without fb-watcher: %+v %v, want a bare-link card", j, err)
	}

	s = newSvc(t)
	s.fbw = &fakeFB{post: fbwatcher.Post{Text: "Tuyển n8n Automation Engineer\nLương 20-30 triệu, Hà Nội", GroupName: "Tuyển Dụng AI Engineer"}}
	j, err = s.AddFromContent(ctx, fbPost)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.FullJD, "n8n Automation Engineer") || j.DiscoverySource != "facebook" || !strings.Contains(j.Notes, "Tuyển Dụng AI Engineer") {
		t.Errorf("job = %+v", j)
	}
	// Text pasted with the link is used as is; fb-watcher isn't asked.
	s.fbw = &fakeFB{err: errors.New("must not be called")}
	if _, err := s.AddFromContent(ctx, "https://www.facebook.com/groups/398/posts/78/ Cần tuyển kế toán kho, lương 10 triệu, Quận 5"); err != nil {
		t.Errorf("link + text: %v", err)
	}
}

// When fb-watcher can't read a post, nothing is added and the user is told
// what to do, in words.
func TestAddFacebookPostFBWatcherRefuses(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{&fbwatcher.Error{Status: 403, Kind: "not_public"}, "private group"},
		{&fbwatcher.Error{Status: 404, Kind: "not_visible"}, "can't see this post"},
		{&fbwatcher.Error{Status: 429, Kind: "rate_limited", RetryAfter: 90 * time.Second}, "try again in 2 minutes"},
		{&fbwatcher.Error{Status: 503, Kind: "profile_busy", RetryAfter: 30 * time.Second}, "try again in 1 minutes"},
		{errors.New("connection refused"), "couldn't reach"},
	} {
		s := newSvc(t)
		s.fbw = &fakeFB{err: c.err}
		_, err := s.AddFromContent(context.Background(), fbPost)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: got %v, want %q", c.err, err, c.want)
		}
		if list, _ := s.ListJobs(context.Background(), ListFilter{}); len(list) != 0 {
			t.Errorf("%v: a job was added", c.err)
		}
	}
}

// Following a group asks fb-watcher at once whether it's public.
func TestFollowGroupChecksAtOnce(t *testing.T) {
	s := newSvc(t)
	f := &fakeFB{group: fbwatcher.Group{Name: "Việc làm AI", Visibility: FBPublic}, called: make(chan string, 1)}
	s.fbw = f
	g, added, err := s.FollowGroup(context.Background(), "https://www.facebook.com/groups/398")
	if err != nil || !added {
		t.Fatal(err)
	}
	select {
	case <-f.called:
	case <-time.After(5 * time.Second):
		t.Fatal("fb-watcher wasn't asked")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _, _ := s.st.FBGroup(context.Background(), g.Key)
		if got.Visibility == FBPublic && got.Name == "Việc làm AI" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("group = %+v, want public", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
