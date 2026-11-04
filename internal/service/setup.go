package service

import (
	"context"
	"fmt"
	"strings"
)

// Getting started: rating only means something once the user has said who
// they are (CV) and what they want, and crawls only find jobs once they have
// search terms. Setup reports those steps so the board can walk a new user
// through them; SaveProfile lets the board edit the profile in place.

// maxProfileChars caps the profile (it goes into every rating request).
const maxProfileChars = 100_000

// SaveProfile replaces the profile's text, private to the user.
func (s *Service) SaveProfile(ctx context.Context, text string) (Profile, error) {
	if s.profilePath == "" {
		return Profile{}, fmt.Errorf("no profile path configured")
	}
	if len([]rune(text)) > maxProfileChars {
		return Profile{}, fmt.Errorf("%w: the profile is over %d characters", ErrInvalid, maxProfileChars)
	}
	if _, err := s.Profile(ctx); err != nil { // creates the data dir on first use
		return Profile{}, err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := writePrivate(s.profilePath, []byte(text)); err != nil {
		return Profile{}, err
	}
	s.logEvent(ctx, "added", "Updated your profile")
	return s.Profile(ctx)
}

// SetupStatus is how far the user is with setting braveduck up. The first
// three steps are needed for useful ratings and crawls; the rest are optional.
type SetupStatus struct {
	CV            bool `json:"cv"`             // a base CV is uploaded
	AboutMe       bool `json:"about_me"`       // the profile says who they are
	Wants         bool `json:"wants"`          // ...and what they're looking for
	SearchQueries int  `json:"search_queries"` // terms the crawlers search for
	Location      bool `json:"location"`       // for job distances
	TopCV         bool `json:"topcv"`          // logged in, for sync and apply
	Done          bool `json:"done"`           // the needed steps are all done
}

// Setup reports the getting-started steps.
func (s *Service) Setup(ctx context.Context) (SetupStatus, error) {
	var st SetupStatus
	if _, ok, err := s.BaseCV(ctx); err != nil {
		return st, err
	} else {
		st.CV = ok
	}
	p, err := s.Profile(ctx)
	if err != nil {
		return st, err
	}
	for heading, body := range profileSections(p.Text) {
		if !profileFilled(body) {
			continue
		}
		if heading == "about me" {
			st.AboutMe = true
		} else {
			st.Wants = true
		}
	}
	kw, err := s.Keywords(ctx)
	if err != nil {
		return st, err
	}
	st.SearchQueries = len(kw["search_queries"])
	if loc, err := s.GetLocation(ctx); err == nil {
		st.Location = loc.Set
	}
	st.TopCV = s.platformConnected(ctx, "topcv")
	st.Done = st.AboutMe && st.Wants && st.SearchQueries > 0
	return st, nil
}

// profileSections splits a profile into its "# " sections, keyed by the
// lowercased heading; text before the first heading is keyed "".
func profileSections(text string) map[string]string {
	out := map[string]string{}
	heading := ""
	var body []string
	flush := func() { out[heading] += strings.Join(body, "\n") }
	for _, line := range strings.Split(htmlCommentRE.ReplaceAllString(text, ""), "\n") {
		if h1RE.MatchString(line) {
			flush()
			heading, body = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "#"))), nil
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}
