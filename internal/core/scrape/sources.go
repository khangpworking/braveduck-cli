package scrape

import (
	"net/url"
	"regexp"
	"strings"
)

// Pasted links are only accepted from the sites braveduck supports. Job sites
// are fetched and their JobPosting read; a Facebook post is never fetched
// (reading it needs the user's login), so its text has to be pasted with it.

// Source is a site whose links the paste box accepts.
type Source struct {
	ID      string   // discovery_source tag
	Name    string   // shown to the user
	Domains []string // the site and its subdomains
	Fetch   bool     // false: the link is stored, the pasted text is read
}

// Sources are the supported sites, in the order they're listed to the user.
var Sources = []Source{
	{ID: "topcv", Name: "TopCV", Domains: []string{"topcv.vn"}, Fetch: true},
	{ID: "vietnamworks", Name: "VietnamWorks", Domains: []string{"vietnamworks.com"}, Fetch: true},
	{ID: "joboko", Name: "JobOKO", Domains: []string{"joboko.com"}, Fetch: true},
	{ID: "careerlink", Name: "CareerLink", Domains: []string{"careerlink.vn"}, Fetch: true},
	{ID: "facebook", Name: "Facebook", Domains: []string{"facebook.com"}},
}

// SourceNames lists the supported sites for messages: "A, B or C".
func SourceNames() string {
	var names []string
	for _, s := range Sources {
		names = append(names, s.Name)
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " or " + names[last]
}

// SupportedLink returns the source of an http(s) link and the link in its
// canonical form (so the same listing pasted twice is caught as a duplicate).
// ok is false for a link from any other site.
func SupportedLink(raw string) (src Source, canonical string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Source{}, "", false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	for _, s := range Sources {
		for _, d := range s.Domains {
			if host == d || strings.HasSuffix(host, "."+d) {
				return s, canonicalize(s, u, host), true
			}
		}
	}
	return Source{}, "", false
}

// fbPermalinkRE matches a group post's /permalink/ form, the same post as /posts/.
var fbPermalinkRE = regexp.MustCompile(`^/groups/([^/]+)/permalink/([^/]+)/?$`)

// canonicalize drops what varies between copies of one link: the fragment and
// tracking query strings (job sites' detail pages are path-based). Facebook
// links also lose the m./web./mbasic. host and end in a slash, matching the
// permalinks fb-watcher stores.
func canonicalize(s Source, u *url.URL, host string) string {
	out := url.URL{Scheme: "https", Host: host, Path: u.Path}
	if s.ID != "facebook" {
		return out.String()
	}
	out.Host = "www.facebook.com"
	if strings.HasSuffix(out.Path, ".php") {
		// permalink.php / story.php carry the post in the query.
		q := url.Values{}
		for _, k := range []string{"story_fbid", "id"} {
			if v := u.Query().Get(k); v != "" {
				q.Set(k, v)
			}
		}
		out.RawQuery = q.Encode()
		return out.String()
	}
	if m := fbPermalinkRE.FindStringSubmatch(out.Path); m != nil {
		out.Path = "/groups/" + m[1] + "/posts/" + m[2] + "/"
	}
	if !strings.HasSuffix(out.Path, "/") {
		out.Path += "/"
	}
	return out.String()
}
