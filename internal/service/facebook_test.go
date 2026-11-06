package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFacebookGroups(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	g, added, err := s.FollowGroup(ctx, "https://m.facebook.com/groups/TuyenDungAI/?ref=share")
	if err != nil || !added || g.Key != "tuyendungai" || g.Visibility != FBChecking {
		t.Fatalf("follow = %+v %v %v", g, added, err)
	}
	if _, added, _ := s.FollowGroup(ctx, "https://www.facebook.com/groups/tuyendungai/"); added {
		t.Fatal("the same group followed twice")
	}
	if _, _, err := s.FollowGroup(ctx, "https://www.facebook.com/groups/1/posts/2/"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a post link followed as a group: %v", err)
	}

	posts := []GroupPost{
		{URL: "https://www.facebook.com/groups/398/posts/1/", Text: "Tuyển n8n Automation Engineer\nLương 20-30 triệu, Hà Nội"},
		{URL: "https://www.facebook.com/groups/398/posts/2/", Text: "Bán xe máy cũ giá rẻ"},
		{URL: "https://www.facebook.com/groups/398/posts/3/", Text: "Tuyển sales n8n part-time"},
		{URL: "https://evil.example/posts/4", Text: "Tuyển n8n dev"},
	}
	// Not known to be public yet: nothing comes in.
	if _, err := s.IngestGroupPosts(ctx, "tuyendungai", posts); !errors.Is(err, ErrGroupNotPublic) {
		t.Fatalf("ingest while checking = %v", err)
	}
	if _, err := s.SetGroupStatus(ctx, "tuyendungai", "Tuyển dụng AI", FBPrivate, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestGroupPosts(ctx, "tuyendungai", posts); !errors.Is(err, ErrGroupNotPublic) {
		t.Fatalf("ingest from a private group = %v", err)
	}
	s.SetGroupStatus(ctx, "tuyendungai", "", FBPublic, "")
	if n, err := s.IngestGroupPosts(ctx, "tuyendungai", posts); err != nil || n != 0 {
		t.Fatalf("no search terms yet: %d %v", n, err)
	}
	addTerms(t, s, "search_queries", "N8N")
	addTerms(t, s, "negative_title", "sales")
	n, err := s.IngestGroupPosts(ctx, "tuyendungai", posts)
	if err != nil || n != 1 {
		t.Fatalf("ingest = %d %v, want 1 (matching, not filtered, a Facebook link)", n, err)
	}
	list, _ := s.ListJobs(ctx, ListFilter{Source: "facebook"})
	if len(list) != 1 || !strings.Contains(list[0].FullJD, "Automation Engineer") || !strings.Contains(list[0].Notes, "Tuyển dụng AI") {
		t.Fatalf("jobs = %+v", list)
	}
	if n, _ := s.IngestGroupPosts(ctx, "tuyendungai", posts); n != 0 {
		t.Fatalf("the same posts again added %d", n)
	}
	gs, _ := s.FBGroups(ctx)
	if len(gs) != 1 || gs[0].Name != "Tuyển dụng AI" || gs[0].Visibility != FBPublic || gs[0].CheckedAt == "" {
		t.Fatalf("groups = %+v", gs)
	}
	if err := s.UnfollowGroup(ctx, "TuyenDungAI"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnfollowGroup(ctx, "tuyendungai"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unfollow twice = %v", err)
	}
}
