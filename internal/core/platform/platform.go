// Package platform is the registry of job sites braveduck can hold a login
// session for (docs/specs/platform-sessions.md §1). A platform is data, not
// code: four built-ins, plus any site the user adds in platforms.json — so the
// feature isn't tied to one country's job boards.
package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Platform describes how to log in to a site and how to tell whether a stored
// session is still logged in.
type Platform struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	LoginURL string   `json:"login_url"`
	Domains  []string `json:"domains"` // cookies/storage kept for these hosts and their subdomains

	// Logged-in probe: open ProbeURL with the session. If the final URL matches
	// LoginPattern the session is logged out. Otherwise ProbeJS (if set) is
	// evaluated in the page: true = logged in, false = logged out, anything
	// else = unknown. With no ProbeJS, "not bounced to login" counts as
	// logged in.
	ProbeURL     string `json:"probe_url"`
	LoginPattern string `json:"login_pattern"`
	ProbeJS      string `json:"probe_js,omitempty"`

	// AuthCookies name the cookies that actually carry the login; their
	// expiry is the "log in again around" hint. Without them no hint is shown
	// (the earliest cookie is often a short-lived helper, not the login).
	AuthCookies []string `json:"auth_cookies,omitempty"`
	// AccountJS (optional) returns a label for the logged-in account, shown
	// masked ("ng…@gmail.com") so the user can tell which account is saved.
	AccountJS string `json:"account_js,omitempty"`

	// Verified is true when the probe is backed by an observed behaviour of
	// the real site (see the spec's Evidence section), not a guess.
	Verified bool `json:"-"`
	Builtin  bool `json:"-"`
}

// Builtin platforms. TopCV's probe is verified (topcv-api-analysis: account
// pages render app.auth.isAuth = true). The others are best guesses until a
// real login confirms them; a wrong guess fails safe as "unknown".
var builtins = []Platform{
	{
		ID: "topcv", Name: "TopCV",
		LoginURL:     "https://www.topcv.vn/login",
		Domains:      []string{"topcv.vn"},
		ProbeURL:     "https://www.topcv.vn/viec-lam-da-luu",
		LoginPattern: `/login`,
		ProbeJS:      `(() => { const a = window.app && window.app.auth; return a ? a.isAuth === true : null })()`,
		// topcv-api-analysis docs/security.md: topcv_session (~90 days) is the
		// Laravel login; sso_token (~12 h) is a short-lived helper.
		AuthCookies: []string{"topcv_session"},
		// window.topcvUser carries the account's email (same doc, §Data exposure).
		// Email first; the name fields are a fallback if the email is absent.
		AccountJS: `(() => { const u = window.topcvUser; return u && (u.email || u.fullname || u.name) || null })()`,
		Verified:  true,
	},
	{
		ID: "vietnamworks", Name: "VietnamWorks",
		LoginURL:     "https://www.vietnamworks.com/dang-nhap",
		Domains:      []string{"vietnamworks.com"},
		ProbeURL:     "https://www.vietnamworks.com/my-career-center",
		LoginPattern: `dang-nhap|/login|secure\.vietnamworks\.com`,
	},
	{
		ID: "joboko", Name: "JobOKO",
		LoginURL:     "https://vn.joboko.com/dang-nhap",
		Domains:      []string{"joboko.com"},
		ProbeURL:     "https://vn.joboko.com/tai-khoan",
		LoginPattern: `dang-nhap|/login`,
	},
	{
		ID: "careerlink", Name: "CareerLink",
		LoginURL:     "https://www.careerlink.vn/nguoi-tim-viec/dang-nhap",
		Domains:      []string{"careerlink.vn"},
		ProbeURL:     "https://www.careerlink.vn/nguoi-tim-viec/viec-lam-cua-toi",
		LoginPattern: `dang-nhap|/login`,
	},
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ErrUnknown is returned by Registry.Get for an id that isn't registered.
var ErrUnknown = errors.New("unknown platform")

// Registry is the set of known platforms.
type Registry struct{ byID map[string]Platform }

// Load returns the built-ins plus the user's platforms.json at path (if it
// exists). A user entry with a built-in's id overrides it, so a user can fix a
// stale login URL without waiting for a release.
func Load(path string) (*Registry, error) {
	r := &Registry{byID: map[string]Platform{}}
	for _, p := range builtins {
		p.Builtin = true
		r.byID[p.ID] = p
	}
	if path == "" {
		return r, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	} else if err != nil {
		return nil, err
	}
	var user []Platform
	if err := json.Unmarshal(b, &user); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, p := range user {
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		r.byID[p.ID] = p
	}
	return r, nil
}

func (p Platform) validate() error {
	if !idRE.MatchString(p.ID) {
		return fmt.Errorf("platform id %q: use lowercase letters, digits, - or _", p.ID)
	}
	for _, u := range []string{p.LoginURL, p.ProbeURL} {
		pu, err := url.Parse(u)
		if err != nil || pu.Host == "" || !(pu.Scheme == "https" || pu.Scheme == "http" && isLoopback(pu.Hostname())) {
			return fmt.Errorf("platform %s: %q must be an https URL (http only for localhost)", p.ID, u)
		}
	}
	if len(p.Domains) == 0 {
		return fmt.Errorf("platform %s: list at least one domain", p.ID)
	}
	for _, u := range []string{p.LoginURL, p.ProbeURL} {
		pu, _ := url.Parse(u)
		if !p.Owns(pu.Hostname()) {
			return fmt.Errorf("platform %s: %s isn't under its domains %v", p.ID, pu.Hostname(), p.Domains)
		}
	}
	if p.LoginPattern == "" {
		return fmt.Errorf("platform %s: login_pattern is required (how to tell it bounced to login)", p.ID)
	}
	if _, err := regexp.Compile(p.LoginPattern); err != nil {
		return fmt.Errorf("platform %s: login_pattern: %w", p.ID, err)
	}
	if p.Name == "" {
		return fmt.Errorf("platform %s: name is required", p.ID)
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Owns reports whether host is one of the platform's domains or a subdomain.
// Only these hosts' cookies and storage are ever captured.
func (p Platform) Owns(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), ".")
	for _, d := range p.Domains {
		d = strings.TrimPrefix(strings.ToLower(d), ".")
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// Get returns the platform with id, or ErrUnknown.
func (r *Registry) Get(id string) (Platform, error) {
	p, ok := r.byID[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return Platform{}, fmt.Errorf("%w %q (known: %s)", ErrUnknown, id, strings.Join(r.IDs(), ", "))
	}
	return p, nil
}

// All returns every platform, sorted by id.
func (r *Registry) All() []Platform {
	out := make([]Platform, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// IDs returns every platform id, sorted.
func (r *Registry) IDs() []string {
	var ids []string
	for _, p := range r.All() {
		ids = append(ids, p.ID)
	}
	return ids
}
