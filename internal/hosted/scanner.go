package hosted

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The Facebook scanner (fb-watcher, logged in to Facebook on its own) talks
// to braveduck on a port of its own, never through Caddy, with its own key:
//
//	GET  /groups              every group some user follows, once
//	POST /groups/{key}/status {"name", "visibility", "note"}
//	POST /groups/{key}/posts  {"posts": [{"url", "text"}]}
//
// braveduck shares nothing beyond the group links. Each follower's board
// takes a public group's posts through its own search terms and filters
// (service/facebook.go); a private group's posts are taken by nobody.

// ScanGroup is one group to scan.
type ScanGroup struct {
	Key        string `json:"key"`
	URL        string `json:"url"`
	Visibility string `json:"visibility"` // the latest anyone's board recorded
	CheckedAt  string `json:"checked_at"`
	Followers  int    `json:"followers"`
}

// Scanner returns the scanner API, answering only requests that carry key.
func (s *Server) Scanner(key string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /groups", s.scanGroups)
	mux.HandleFunc("POST /groups/{key}/status", s.scanStatus)
	mux.HandleFunc("POST /groups/{key}/posts", s.scanPosts)
	mux.HandleFunc("POST /health", s.scanHealth)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeErr(w, http.StatusUnauthorized, "the scanner key is missing or wrong")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		mux.ServeHTTP(w, r)
	})
}

// tenants returns every open board.
func (s *Server) tenants() []*tenant {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*tenant, 0, len(s.users))
	for _, t := range s.users {
		out = append(out, t)
	}
	return out
}

func (s *Server) scanGroups(w http.ResponseWriter, r *http.Request) {
	byKey := map[string]*ScanGroup{}
	var order []string
	for _, t := range s.tenants() {
		if !t.svc.SourceOn(r.Context(), "facebook") {
			continue // this board turned Facebook off
		}
		gs, err := t.svc.FBGroups(r.Context())
		if err != nil {
			continue
		}
		for _, g := range gs {
			if g.Paused {
				continue
			}
			sg, ok := byKey[g.Key]
			if !ok {
				sg = &ScanGroup{Key: g.Key, URL: g.URL, Visibility: g.Visibility, CheckedAt: g.CheckedAt}
				byKey[g.Key] = sg
				order = append(order, g.Key)
			}
			sg.Followers++
			if g.CheckedAt > sg.CheckedAt {
				sg.Visibility, sg.CheckedAt = g.Visibility, g.CheckedAt
			}
		}
	}
	out := make([]ScanGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) scanStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, Visibility, Note string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return
	}
	key := strings.ToLower(r.PathValue("key"))
	n := 0
	for _, t := range s.tenants() {
		updated, err := t.svc.SetGroupStatus(r.Context(), key, in.Name, in.Visibility, in.Note)
		if errors.Is(err, service.ErrInvalid) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if updated {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"boards": n})
}

func (s *Server) scanPosts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Posts []service.GroupPost `json:"posts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return
	}
	key := strings.ToLower(r.PathValue("key"))
	added, boards := 0, 0
	for _, t := range s.tenants() {
		n, err := t.svc.IngestGroupPosts(r.Context(), key, in.Posts)
		if errors.Is(err, service.ErrGroupNotPublic) {
			continue // not following it, or not a public group
		}
		boards++
		added += n
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added, "boards": boards})
}

// scanHealth records how the scanner's round went on every board it reads
// for (Facebook on, a group not paused): {"ok": true} when its scans worked,
// {"ok": false, "error": "…"} when they didn't. A scanner that stops sending
// it shows on the board as Facebook going quiet; one that sends failures
// shows the failure. Asking for the groups proves nothing about the scans.
func (s *Server) scanHealth(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.OK == nil {
		writeErr(w, http.StatusBadRequest, `expected {"ok": true|false, "error": "…"}`)
		return
	}
	reason := ""
	if !*in.OK {
		reason = strings.TrimSpace(in.Error)
		if reason == "" {
			reason = "the Facebook scanner's round failed"
		}
	}
	boards := 0
	for _, t := range s.tenants() {
		if !t.svc.SourceOn(r.Context(), "facebook") {
			continue
		}
		gs, err := t.svc.FBGroups(r.Context())
		if err != nil || !slices.ContainsFunc(gs, func(g service.FBGroup) bool { return !g.Paused }) {
			continue
		}
		if err := t.svc.ReportCrawl(r.Context(), "facebook", *in.OK, reason); err == nil {
			boards++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"boards": boards})
}
