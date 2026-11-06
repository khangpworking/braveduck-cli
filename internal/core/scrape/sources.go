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
	query := u.Query()
	// A group link that opens one post (?multi_permalinks=P) is that post.
	if m := fbGroupRE.FindStringSubmatch(out.Path); m != nil && query.Get("multi_permalinks") != "" {
		return "https://www.facebook.com/groups/" + m[1] + "/posts/" + strings.Split(query.Get("multi_permalinks"), ",")[0] + "/"
	}
	if m := fbPermalinkRE.FindStringSubmatch(out.Path); m != nil {
		out.Path = "/groups/" + m[1] + "/posts/" + m[2] + "/"
	}
	if !strings.HasSuffix(out.Path, "/") && !strings.HasSuffix(out.Path, ".php") {
		out.Path += "/"
	}
	// Keep only the parameters that name the post (permalink.php, story.php,
	// photo.php, /watch/?v=); tracking parameters go.
	q := url.Values{}
	for _, k := range fbPostParams {
		if v := query.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	out.RawQuery = q.Encode()
	return out.String()
}

// fbPostParams are the query parameters that identify a Facebook post.
var fbPostParams = []string{"story_fbid", "id", "fbid", "v"}

// fbGroupRE matches a bare group link: /groups/<id>/.
var fbGroupRE = regexp.MustCompile(`^/groups/([^/]+)/?$`)

// FacebookGroup reports whether raw is a link to a Facebook group itself
// (not one of its posts), and returns the group's key (its id or name, as
// in facebook.com/groups/<key>) and its canonical link.
func FacebookGroup(raw string) (key, link string, ok bool) {
	src, canonical, ok := SupportedLink(raw)
	if !ok || src.ID != "facebook" {
		return "", "", false
	}
	u, err := url.Parse(canonical)
	if err != nil || u.RawQuery != "" {
		return "", "", false
	}
	m := fbGroupRE.FindStringSubmatch(u.Path)
	if m == nil || !fbGroupKeyRE.MatchString(m[1]) {
		return "", "", false
	}
	key = strings.ToLower(m[1])
	return key, "https://www.facebook.com/groups/" + key + "/", true
}

// fbGroupKeyRE is what a group's id or name may look like.
var fbGroupKeyRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
