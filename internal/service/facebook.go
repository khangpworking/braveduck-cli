package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Facebook groups: the user pastes a group's link to follow it. braveduck
// never opens Facebook itself; a scanner (fb-watcher) reads the groups and
// reports each one's name and whether it's public, then hands over its new
// posts. Only a public group's posts become jobs: anyone may read those,
// while a private group's posts are for its members, and braveduck can't
// tell who is one. Each user's own search terms and filters pick the posts.

// FBGroup is a followed group, as the board shows it.
type FBGroup = store.FBGroup

// Group visibility, as the scanner reports it.
const (
	FBChecking    = "checking"
	FBPublic      = "public"
	FBPrivate     = "private"
	FBUnavailable = "unavailable" // gone, renamed, or the scanner couldn't read it
)

// MaxFBGroups caps the groups one board follows (every group is a scan).
const MaxFBGroups = 30

// GroupPost is one post the scanner read from a group.
type GroupPost struct {
	URL  string `json:"url"`  // its permalink
	Text string `json:"text"` // the post's text
}

// FollowGroup follows the Facebook group at link. added is false when it was
// followed already.
func (s *Service) FollowGroup(ctx context.Context, link string) (g FBGroup, added bool, err error) {
	key, url, ok := scrape.FacebookGroup(link)
	if !ok {
		return FBGroup{}, false, fmt.Errorf("%w: that isn't a link to a Facebook group (facebook.com/groups/…)", ErrInvalid)
	}
	all, err := s.st.FBGroups(ctx)
	if err != nil {
		return FBGroup{}, false, err
	}
	if len(all) >= MaxFBGroups {
		return FBGroup{}, false, fmt.Errorf("%w: you follow %d groups already — unfollow one first", ErrInvalid, len(all))
	}
	if added, err = s.st.AddFBGroup(ctx, key, url, s.now()); err != nil {
		return FBGroup{}, false, err
	}
	g, _, err = s.st.FBGroup(ctx, key)
	if added {
		s.logEvent(ctx, "added", "Following the Facebook group "+url)
		s.checkGroupSoon(key, url)
	}
	return g, added, err
}

// PauseGroup turns a followed group off (paused) or back on: while it's off
// the scanner doesn't read it for this board and its posts aren't taken.
func (s *Service) PauseGroup(ctx context.Context, key string, paused bool) (FBGroup, error) {
	key = strings.ToLower(key)
	updated, err := s.st.SetFBGroupPaused(ctx, key, paused)
	if err != nil {
		return FBGroup{}, err
	}
	if !updated {
		return FBGroup{}, fmt.Errorf("%w: not following that group", ErrNotFound)
	}
	g, _, err := s.st.FBGroup(ctx, key)
	return g, err
}

// FBGroups lists the followed groups.
func (s *Service) FBGroups(ctx context.Context) ([]FBGroup, error) { return s.st.FBGroups(ctx) }

// UnfollowGroup stops following a group (its jobs stay on the board).
func (s *Service) UnfollowGroup(ctx context.Context, key string) error {
	deleted, err := s.st.DeleteFBGroup(ctx, strings.ToLower(key))
	if err == nil && !deleted {
		err = fmt.Errorf("%w: not following that group", ErrNotFound)
	}
	if err == nil {
		s.logEvent(ctx, "info", "Stopped following a Facebook group")
	}
	return err
}

// SetGroupStatus records what the scanner found about a followed group.
// updated is false when this board doesn't follow it.
func (s *Service) SetGroupStatus(ctx context.Context, key, name, visibility, note string) (updated bool, err error) {
	switch visibility {
	case FBPublic, FBPrivate, FBUnavailable:
	default:
		return false, fmt.Errorf("%w: visibility %q", ErrInvalid, visibility)
	}
	g, ok, err := s.st.FBGroup(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if len([]rune(name)) > 200 {
		name = string([]rune(name)[:200])
	}
	if len([]rune(note)) > 300 {
		note = string([]rune(note)[:300])
	}
	if err := s.st.SetFBGroupStatus(ctx, key, name, visibility, note, s.now()); err != nil {
		return false, err
	}
	if g.Visibility != visibility && visibility == FBPrivate {
		s.logEvent(ctx, "info", fmt.Sprintf("%s is a private group: only its members can see its posts, so paste the ones you want", nonEmpty(name, g.URL)))
	}
	return true, nil
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ErrGroupNotPublic means posts were offered for a group this board doesn't
// know to be public.
var ErrGroupNotPublic = errors.New("not a followed public group")

// MaxGroupPosts caps the posts taken from one group in one hand-over.
const MaxGroupPosts = 200

// IngestGroupPosts adds the posts from a followed public group that match
// the user's search terms and filters; the rest are skipped, as are posts
// already on the board.
func (s *Service) IngestGroupPosts(ctx context.Context, key string, posts []GroupPost) (added int, err error) {
	g, ok, err := s.st.FBGroup(ctx, key)
	if err != nil {
		return 0, err
	}
	if !ok || g.Visibility != FBPublic {
		return 0, ErrGroupNotPublic
	}
	if g.Paused || !s.SourceOn(ctx, "facebook") {
		return 0, nil // turned off: its posts don't come onto the board
	}
	sets, err := s.st.AllKeywords(ctx)
	if err != nil {
		return 0, err
	}
	var terms []string
	for _, t := range sets["search_queries"] {
		if t = strings.TrimSpace(searchFold(t)); t != "" {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return 0, nil // nothing to look for yet
	}
	board, err := s.loadDupIndex(ctx)
	if err != nil {
		return 0, err
	}
	if len(posts) > MaxGroupPosts {
		posts = posts[:MaxGroupPosts]
	}
	for _, p := range posts {
		src, link, ok := scrape.SupportedLink(p.URL)
		text := strings.TrimSpace(p.Text)
		if !ok || src.ID != "facebook" || text == "" {
			continue // the scanner names the group; a post needs its own Facebook link
		}
		if len(text) > maxPostChars {
			text = text[:maxPostChars]
		}
		hay := searchFold(text)
		matched := false
		for _, t := range terms {
			if strings.Contains(hay, t) {
				matched = true
				break
			}
		}
		jp := scrape.ParsePost(text)
		if !matched || !keyword.Filter(keyword.Job{Title: jp.Title, Description: text, Company: jp.Company}, sets).Keep {
			continue
		}
		j := jobs.Job{
			Title: nonEmpty(jp.Title, "Facebook post"), Company: jp.Company, Location: jp.Location,
			Salary: jp.Salary, URL: link, FullJD: text, DiscoverySource: "facebook",
			Notes: "From the Facebook group " + nonEmpty(g.Name, g.URL),
		}
		if len(board.find(j)) > 0 {
			continue
		}
		saved, err := s.addQuiet(ctx, j)
		if err != nil {
			if errors.Is(err, ErrInvalid) { // e.g. the board is full: stop, don't drop silently
				return added, err
			}
			continue
		}
		board.add(saved)
		added++
	}
	if added > 0 {
		s.logEvent(ctx, "added", fmt.Sprintf("%s: %d new job post(s)", nonEmpty(g.Name, "Facebook group"), added))
	}
	return added, nil
}

// maxPostChars caps one post's text.
const maxPostChars = 20_000
