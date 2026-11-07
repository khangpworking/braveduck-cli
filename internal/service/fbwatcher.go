package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/fbwatcher"
)

// fb-watcher (khangpworking/fb-watcher) reads Facebook for braveduck, which
// never opens Facebook itself: a pasted post link becomes a job without
// pasting its text, and a followed group shows at once whether it's public.
// fb-watcher only reads public groups and limits how often it reads.

// fbReader is fbwatcher.Client; a fake in tests.
type fbReader interface {
	FetchPost(ctx context.Context, link string) (fbwatcher.Post, error)
	CheckGroup(ctx context.Context, link string) (fbwatcher.Group, error)
}

// SetFBWatcher connects fb-watcher's API (nil disconnects it).
func (s *Service) SetFBWatcher(c *fbwatcher.Client) {
	if c == nil {
		s.fbw = nil
		return
	}
	s.fbw = c
}

// readFacebookPost asks fb-watcher for the post at link, and says in plain
// words what to do when it can't be read.
func (s *Service) readFacebookPost(ctx context.Context, link string) (fbwatcher.Post, error) {
	post, err := s.fbw.FetchPost(ctx, link)
	if err == nil && len(post.Text) >= 20 {
		return post, nil
	}
	if err == nil {
		return post, fmt.Errorf("%w: this Facebook post has no text to read — paste the job's text under the link", ErrInvalid)
	}
	var e *fbwatcher.Error
	if !errors.As(err, &e) {
		log.Printf("fb-watcher: %v", err)
		return post, fmt.Errorf("%w: braveduck couldn't reach its Facebook reader — paste the post's text under the link", ErrInvalid)
	}
	switch e.Kind {
	case "not_public":
		return post, fmt.Errorf("%w: this post is in a private group, so braveduck can't read it — paste its text under the link", ErrInvalid)
	case "not_visible", "not_found":
		return post, fmt.Errorf("%w: braveduck can't see this post (deleted, or for members only) — paste its text under the link", ErrInvalid)
	case "bad_url":
		return post, fmt.Errorf("%w: that isn't a link to a post in a Facebook group", ErrInvalid)
	case "rate_limited", "profile_busy", "login_required", "timeout":
		return post, fmt.Errorf("%w: reading Facebook is busy right now — try again in %d minutes, or paste the post's text under the link", ErrInvalid, minutes(e.RetryAfter))
	}
	log.Printf("fb-watcher: %v", e)
	return post, fmt.Errorf("%w: braveduck couldn't read this post — paste its text under the link", ErrInvalid)
}

func minutes(d time.Duration) int {
	if d <= 0 {
		return 5
	}
	return int(math.Ceil(d.Minutes()))
}

// checkGroupSoon asks fb-watcher, in the background, whether a newly
// followed group is public, so the board says so at once instead of after
// the scanner's next round.
func (s *Service) checkGroupSoon(key, url string) {
	if s.fbw == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		g, err := s.fbw.CheckGroup(ctx, url)
		if err != nil {
			log.Printf("fb-watcher: check %s: %v", url, err) // the scanner checks it later
			return
		}
		switch g.Visibility {
		case FBPublic, FBPrivate, FBUnavailable:
		default:
			return
		}
		if _, err := s.SetGroupStatus(ctx, key, g.Name, g.Visibility, g.Note); err != nil {
			log.Printf("fb-watcher: record %s: %v", url, err)
		}
	}()
}
