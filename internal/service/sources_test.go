package service

import (
	"context"
	"errors"
	"testing"
)

func TestTurningASourceOff(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	on := func(id string) bool {
		t.Helper()
		list, err := s.BoardSources(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range list {
			if b.Source == id {
				return b.On
			}
		}
		t.Fatalf("%s isn't listed in %+v", id, list)
		return false
	}
	for _, id := range []string{"topcv", "vietnamworks", "careerlink", "joboko", "facebook"} {
		if !on(id) {
			t.Errorf("%s starts off", id)
		}
	}
	if _, err := s.SetSourceOn(ctx, "jev-rate", false); !errors.Is(err, ErrInvalid) {
		t.Errorf("a background task turned off as a source: %v", err)
	}

	if _, err := s.SetSourceOn(ctx, "facebook", false); err != nil || on("facebook") {
		t.Fatalf("turn Facebook off: %v", err)
	}
	// A tool feeding the board is refused; the user's own paste isn't.
	if _, err := s.AddJob(ctx, NewJob{Title: "From fb-watcher", Source: "facebook", URL: "https://www.facebook.com/groups/1/posts/2/"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a job from a source that's off: %v", err)
	}
	if _, err := s.AddJob(ctx, NewJob{Title: "Pasted", URL: "https://www.facebook.com/groups/1/posts/3/"}); err != nil {
		t.Fatalf("a link the user pasted: %v", err)
	}
	// Its silence isn't a failure.
	s.ReportCrawl(ctx, "facebook", false, "BrowserUnavailableError")
	v, _ := s.CrawlStatus(ctx)
	for _, src := range v.Sources {
		if src.Name == "facebook" && !src.Healthy {
			t.Error("a source that's off is flagged as failing")
		}
	}
	// A followed group's posts aren't taken while Facebook is off.
	s.FollowGroup(ctx, "https://www.facebook.com/groups/aijobs/")
	s.SetGroupStatus(ctx, "aijobs", "AI jobs", FBPublic, "")
	addTerms(t, s, "search_queries", "n8n")
	posts := []GroupPost{{URL: "https://www.facebook.com/groups/398/posts/1/", Text: "Tuyển n8n Automation Engineer\nLương 20-30 triệu"}}
	if n, err := s.IngestGroupPosts(ctx, "aijobs", posts); err != nil || n != 0 {
		t.Fatalf("posts taken while Facebook is off: %d, %v", n, err)
	}

	if _, err := s.SetSourceOn(ctx, "facebook", true); err != nil || !on("facebook") {
		t.Fatalf("turn Facebook back on: %v", err)
	}
	// One group can be turned off on its own, and back on.
	if g, err := s.PauseGroup(ctx, "AIJobs", true); err != nil || !g.Paused {
		t.Fatalf("pause = %+v, %v", g, err)
	}
	if n, _ := s.IngestGroupPosts(ctx, "aijobs", posts); n != 0 {
		t.Fatalf("a paused group's posts were taken: %d", n)
	}
	s.PauseGroup(ctx, "aijobs", false)
	if n, err := s.IngestGroupPosts(ctx, "aijobs", posts); err != nil || n != 1 {
		t.Fatalf("back on: %d, %v", n, err)
	}
	if _, err := s.PauseGroup(ctx, "nobody", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pausing a group nobody follows: %v", err)
	}
}

func TestACrawlerThatsOffDoesntRun(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	ids := s.BuiltinCrawlers()
	if len(ids) == 0 {
		t.Skip("no built-in crawlers")
	}
	s.SetSourceOn(ctx, ids[0], false)
	if _, err := s.Crawl(ctx, ids[0]); !errors.Is(err, ErrInvalid) {
		t.Fatalf("crawling a source that's off: %v", err)
	}
}
