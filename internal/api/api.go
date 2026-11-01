// Package api is the HTTP face over the shared core and store: a small JSON
// REST surface plus the embedded web board. It's what `braveduck serve` boots,
// and the contract the (separately built, then vendored) front-end talks to.
//
// The router is stdlib net/http with Go 1.22 method+path patterns — no
// framework, matching the CLI's dependency-free stance. Every handler is a thin
// translation between JSON and the store; all real logic lives in the core.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

//go:embed web
var webFS embed.FS

// Handler builds the http.Handler serving the API and the embedded board.
//
// token is a per-run secret (ARCHITECTURE.md §1.3) that every /api/ request
// must present as "Authorization: Bearer <token>". The board page gets it
// injected at serve time; other sites can't read it (same-origin policy) and
// can't send a custom header cross-origin without a CORS preflight we never
// grant — so neither a malicious page nor a CSRF form can drive the API. The
// Host check closes the remaining hole: a DNS-rebinding page *is* same-origin
// with us, but its Host header names the attacker's domain, not localhost.
func Handler(st *store.Store, token string) http.Handler {
	if token == "" {
		panic("api.Handler: empty token")
	}
	s := &server{svc: service.New(st)}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.createJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("PATCH /api/jobs/{id}", s.patchJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.deleteJob)
	mux.HandleFunc("POST /api/jobs/{id}/pin", s.togglePin)
	mux.HandleFunc("POST /api/jobs/{id}/keep", s.toggleKeep)
	mux.HandleFunc("GET /api/stats", s.getStats)
	mux.HandleFunc("GET /api/keywords", s.getKeywords)
	mux.HandleFunc("GET /api/rating-config", s.getRatingConfig)
	mux.HandleFunc("PUT /api/rating-config", s.putRatingConfig)
	mux.HandleFunc("GET /api/crawl-status", s.getCrawlStatus)
	mux.HandleFunc("GET /api/activity", s.getActivity)
	mux.HandleFunc("POST /api/crawl/{source}/retry", s.retryCrawl)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Everything else: the embedded web board (index.html + assets), with the
	// token injected into index.html.
	sub, _ := fs.Sub(webFS, "web")
	index := renderIndex(sub, token)
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("GET /index.html", index)
	mux.Handle("GET /", files)

	return guard(token, mux)
}

// tokenMeta is the placeholder in web/index.html that renderIndex fills.
const tokenMeta = `<meta name="braveduck-token" content="">`

// renderIndex serves index.html with the per-run token filled in. Never cached:
// a stale page would carry a dead token after a restart.
func renderIndex(web fs.FS, token string) http.HandlerFunc {
	raw, err := fs.ReadFile(web, "index.html")
	if err != nil || !bytes.Contains(raw, []byte(tokenMeta)) {
		panic("api: web/index.html is missing the braveduck-token meta placeholder")
	}
	page := bytes.Replace(raw, []byte(tokenMeta),
		[]byte(`<meta name="braveduck-token" content="`+html.EscapeString(token)+`">`), 1)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	}
}

// guard wraps every request: loopback Host only, a bearer token on /api/, and
// headers that stop the board being framed by another site (clickjacking).
func guard(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			writeErr(w, http.StatusForbidden, errors.New("braveduck only answers on localhost"))
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if strings.HasPrefix(r.URL.Path, "/api/") &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeErr(w, http.StatusUnauthorized, errors.New("missing or bad token — reload the board"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether a Host header names this machine's loopback
// (localhost, 127.0.0.0/8 or ::1), with or without a port.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type server struct{ svc *service.Service }

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListJobs(r.Context(), service.ListFilter{
		Status: r.URL.Query().Get("status"),
		Source: r.URL.Query().Get("source"),
	})
	reply(w, http.StatusOK, list, err)
}

// createReq is the Extract & Add payload: a single pasted string that is either
// a listing URL or free-text JD.
type createReq struct {
	Content string `json:"content"`
}

func (s *server) createJob(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if !decode(w, r, &req) {
		return
	}
	j, err := s.svc.AddFromContent(r.Context(), req.Content)
	reply(w, http.StatusCreated, j, err)
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		j, err := s.svc.GetJob(r.Context(), id)
		reply(w, http.StatusOK, j, err)
	}
}

// jobPatch is the mutable subset a board edits. Pointers distinguish "field
// omitted" from a zero value, so a PATCH touches only what it names.
type jobPatch struct {
	Status *string `json:"status"`
	Pinned *bool   `json:"pinned"`
	Keep   *bool   `json:"keep"`
	Notes  *string `json:"notes"`
}

func (s *server) patchJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var p jobPatch
	if !decode(w, r, &p) {
		return
	}
	j, err := s.svc.UpdateJob(r.Context(), id, service.Patch(p))
	reply(w, http.StatusOK, j, err)
}

func (s *server) deleteJob(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		_, err := s.svc.DeleteJob(r.Context(), id)
		reply(w, http.StatusOK, map[string]bool{"success": true}, err)
	}
}

// togglePin and toggleKeep flip one flag and return the updated job; they are
// the §1.5 contract's POST /pin and /keep, which the board's buttons call.
func (s *server) togglePin(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		j, err := s.svc.TogglePin(r.Context(), id)
		reply(w, http.StatusOK, j, err)
	}
}

func (s *server) toggleKeep(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		j, err := s.svc.ToggleKeep(r.Context(), id)
		reply(w, http.StatusOK, j, err)
	}
}

// ratingConfigJSON is the board's wire shape for rating.Config.
type ratingConfigJSON struct {
	DefaultModel string   `json:"default_model"`
	ABMode       bool     `json:"ab_mode"`
	Models       []string `json:"models"`
}

func toRatingJSON(c rating.Config) ratingConfigJSON {
	return ratingConfigJSON{DefaultModel: c.DefaultModel, ABMode: c.ABEnabled, Models: c.Models}
}

func (s *server) getRatingConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.RatingConfig(r.Context())
	reply(w, http.StatusOK, toRatingJSON(c), err)
}

func (s *server) putRatingConfig(w http.ResponseWriter, r *http.Request) {
	var in ratingConfigJSON
	if !decode(w, r, &in) {
		return
	}
	c, err := s.svc.SetRatingConfig(r.Context(), rating.Config{
		ABEnabled: in.ABMode, Models: in.Models, DefaultModel: in.DefaultModel,
	})
	reply(w, http.StatusOK, toRatingJSON(c), err)
}

// getCrawlStatus reports per-source crawl health and today's find count. The
// source rows come from crawl_runs (empty until a crawler is ported); the
// today-count is real from the jobs table regardless.
func (s *server) getCrawlStatus(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.CrawlStatus(r.Context())
	reply(w, http.StatusOK, v, err)
}

// getActivity returns the recent activity feed (server-backed, so it survives a
// browser change and captures every client's actions). Newest first, capped.
func (s *server) getActivity(w http.ResponseWriter, r *http.Request) {
	ev, err := s.svc.Activity(r.Context(), 20)
	reply(w, http.StatusOK, ev, err)
}

// retryCrawl is an honest not-implemented stub: crawlers aren't ported into
// braveduck-cli yet (they need the captured-session/browser layer), so there's
// no run to re-trigger. Returns 501 with a clear message rather than faking
// success, so the board can tell the user the truth.
func (s *server) retryCrawl(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"ok":      false,
		"source":  r.PathValue("source"),
		"message": "crawlers aren't wired into braveduck-cli yet — nothing to retry",
	})
}

// getStats is the funnel + interview-rate widget data (§1.5).
func (s *server) getStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Stats(r.Context())
	reply(w, http.StatusOK, st, err)
}

func (s *server) getKeywords(w http.ResponseWriter, r *http.Request) {
	sets, err := s.svc.Keywords(r.Context())
	reply(w, http.StatusOK, sets, err)
}

// --- helpers ---

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("bad job id"))
		return 0, false
	}
	return id, true
}

// decode reads a JSON body (capped at 1 MiB), answering 400 on failure.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

// reply writes v with code on success, or maps a service error to its status.
func reply(w http.ResponseWriter, code int, v any, err error) {
	switch {
	case err == nil:
		writeJSON(w, code, v)
	case errors.Is(err, context.Canceled):
		// client went away; nothing to write
	case errors.Is(err, service.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, service.ErrDuplicate):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, service.ErrInvalid):
		writeErr(w, http.StatusBadRequest, err)
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
