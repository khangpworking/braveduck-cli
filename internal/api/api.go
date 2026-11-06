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
	"fmt"
	"html"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/keyword"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
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
//
// svc is the configured service (data dir set, so sessions and the profile
// resolve); st is its store, which the live-update watcher polls.
func Handler(st *store.Store, svc *service.Service, token string) http.Handler {
	if token == "" {
		panic("api.Handler: empty token")
	}
	s := &server{svc: svc, hub: newHub(st)}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.createJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("PATCH /api/jobs/{id}", s.patchJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.deleteJob)
	mux.HandleFunc("POST /api/jobs/{id}/rate", s.rateRequest)
	mux.HandleFunc("POST /api/jobs/{id}/cv", s.cvRequest)
	mux.HandleFunc("PUT /api/jobs/{id}/cv", s.putCV)
	mux.HandleFunc("GET /api/jobs/{id}/cv", s.getCV)
	mux.HandleFunc("GET /api/jobs/{id}/cv.pdf", s.cvPDF)
	mux.HandleFunc("POST /api/jobs/{id}/draft", s.draftRequest)
	mux.HandleFunc("PUT /api/jobs/{id}/draft", s.putDraft)
	mux.HandleFunc("POST /api/jobs/{id}/draft/sent", s.draftSent)
	mux.HandleFunc("POST /api/jobs/{id}/draft/reject", s.draftReject)
	mux.HandleFunc("POST /api/jobs/{id}/apply", s.applyJob)
	mux.HandleFunc("POST /api/jobs/{id}/apply/confirm", s.applyConfirm)
	mux.HandleFunc("POST /api/jobs/{id}/pin", s.togglePin)
	mux.HandleFunc("POST /api/jobs/{id}/keep", s.toggleKeep)
	mux.HandleFunc("GET /api/profile", s.getProfile)
	mux.HandleFunc("PUT /api/profile", s.putProfile)
	mux.HandleFunc("GET /api/profile/form", s.getProfileForm)
	mux.HandleFunc("PUT /api/profile/form", s.putProfileForm)
	mux.HandleFunc("GET /api/setup", s.getSetup)
	mux.HandleFunc("GET /api/today", s.getToday)
	mux.HandleFunc("GET /api/export.csv", s.exportCSV)
	mux.HandleFunc("GET /api/cv", s.getBaseCV)
	mux.HandleFunc("POST /api/cv", s.uploadBaseCV)
	mux.HandleFunc("GET /api/cv/file", s.baseCVFile)
	mux.HandleFunc("GET /api/stats", s.getStats)
	mux.HandleFunc("GET /api/facebook-groups", s.listFBGroups)
	mux.HandleFunc("DELETE /api/facebook-groups/{key}", s.unfollowFBGroup)
	mux.HandleFunc("GET /api/agent-key", s.getAgentKey)
	mux.HandleFunc("POST /api/agent-key", s.newAgentKey)
	mux.HandleFunc("DELETE /api/agent-key", s.revokeAgentKey)
	mux.HandleFunc("GET /api/location", s.getLocation)
	mux.HandleFunc("PUT /api/location", s.putLocation)
	mux.HandleFunc("DELETE /api/location", s.deleteLocation)
	mux.HandleFunc("GET /api/places", s.searchPlaces)
	mux.HandleFunc("GET /api/platforms", s.listPlatforms)
	mux.HandleFunc("POST /api/platforms/{name}/login", s.connectPlatform)
	mux.HandleFunc("DELETE /api/platforms/{name}/login", s.disconnectPlatform)
	mux.HandleFunc("POST /api/platforms/{name}/sync", s.syncPlatform)
	mux.HandleFunc("GET /api/keywords", s.getKeywords)
	mux.HandleFunc("PUT /api/keywords", s.putKeywords)
	mux.HandleFunc("GET /api/crawl-schedule", s.getCrawlSchedule)
	mux.HandleFunc("PUT /api/crawl-schedule/{source}", s.putCrawlSchedule)
	mux.HandleFunc("GET /api/rating-config", s.getRatingConfig)
	mux.HandleFunc("PUT /api/rating-config", s.putRatingConfig)
	mux.HandleFunc("GET /api/crawl-status", s.getCrawlStatus)
	mux.HandleFunc("GET /api/activity", s.getActivity)
	mux.HandleFunc("GET /api/events", s.events)
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

// BoardLang is the board's default language: BRAVEDUCK_LANG ("vi" or
// "en"), Vietnamese when unset.
func BoardLang() string {
	if v := os.Getenv("BRAVEDUCK_LANG"); v == "en" || v == "vi" {
		return v
	}
	return "vi"
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
	// The board's language until the user picks one (⚙ Settings).
	page = bytes.Replace(page, []byte(`<html lang="vi">`), []byte(`<html lang="`+BoardLang()+`">`), 1)
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

		auth := r.Header.Get("Authorization")
		// EventSource can't set headers, so the event stream alone may carry
		// the token in the query (ARCHITECTURE.md §1.5 allows ?token=).
		if r.URL.Path == "/api/events" && auth == "" {
			auth = "Bearer " + r.URL.Query().Get("token")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") &&
			subtle.ConstantTimeCompare([]byte(auth), want) != 1 {
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

type server struct {
	svc *service.Service
	hub *hub
}

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
	// A Facebook group's link follows the group instead of adding a job.
	if _, _, ok := scrape.FacebookGroup(strings.TrimSpace(req.Content)); ok {
		g, added, err := s.svc.FollowGroup(r.Context(), strings.TrimSpace(req.Content))
		reply(w, http.StatusCreated, map[string]any{"group": g, "added": added}, err)
		return
	}
	j, err := s.svc.AddFromContent(r.Context(), req.Content)
	var dup *service.DuplicateError
	if errors.As(err, &dup) {
		// Name the jobs it matches, so the board can point at them.
		type match struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		out := struct {
			Error      string  `json:"error"`
			Duplicates []match `json:"duplicates"`
		}{Error: err.Error()}
		for _, d := range dup.Jobs {
			out.Duplicates = append(out.Duplicates, match{d.ID, d.Title, d.Status})
		}
		writeJSON(w, http.StatusConflict, out)
		return
	}
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
	Status   *string `json:"status"`
	Pinned   *bool   `json:"pinned"`
	Keep     *bool   `json:"keep"`
	Notes    *string `json:"notes"`
	Title    *string `json:"title"`
	Company  *string `json:"company"`
	Salary   *string `json:"salary"`
	Location *string `json:"location"`
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
	j, err := s.svc.UpdateJob(r.Context(), id, service.Patch{
		Status: p.Status, Pinned: p.Pinned, Keep: p.Keep, Notes: p.Notes,
		Title: p.Title, Company: p.Company, Salary: p.Salary, Location: p.Location,
	})
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

// retryCrawl runs a built-in crawler now (the board's "Retry" / "Check now").
// External sources run in their own tool, so there's nothing to start here;
// that's a 501 with a clear message rather than a faked success.
func (s *server) retryCrawl(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if !s.svc.IsBuiltinSource(source) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"ok":      false,
			"source":  source,
			"message": "braveduck can't restart this source — it's run by its own tool (e.g. fb-watcher); restart it there",
		})
		return
	}
	// Finish the crawl even if the board tab goes away mid-request.
	res, err := s.svc.Crawl(context.WithoutCancel(r.Context()), source)
	if errors.Is(err, service.ErrCrawlRunning) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	if err != nil && !errors.Is(err, service.ErrInvalid) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "source": source, "message": err.Error()})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "source": source, "result": res,
		"message": crawlMessage(source, res)}, err)
}

// crawlMessage sums up a crawl for the board's toast: what's new, and what
// was found but skipped (already on the board, or filtered out).
func crawlMessage(source string, res service.CrawlResult) string {
	msg := fmt.Sprintf("%d new job(s) from %s", res.Added, source)
	if res.Duplicates > 0 {
		msg += fmt.Sprintf(" · %d already on the board", res.Duplicates)
	}
	if res.Filtered > 0 {
		msg += fmt.Sprintf(" · %d filtered out", res.Filtered)
	}
	return msg
}

func (s *server) putKeywords(w http.ResponseWriter, r *http.Request) {
	var sets keyword.Sets
	if !decode(w, r, &sets) {
		return
	}
	out, err := s.svc.SetKeywords(r.Context(), sets)
	reply(w, http.StatusOK, out, err)
}

func (s *server) getCrawlSchedule(w http.ResponseWriter, r *http.Request) {
	scheds, err := s.svc.CrawlSchedules(r.Context())
	reply(w, http.StatusOK, scheds, err)
}

func (s *server) putCrawlSchedule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EveryMinutes int `json:"every_minutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	sc, err := s.svc.SetCrawlSchedule(r.Context(), r.PathValue("source"), in.EveryMinutes)
	reply(w, http.StatusOK, sc, err)
}

// listPlatforms is each job site's login status (never session data).
func (s *server) listPlatforms(w http.ResponseWriter, r *http.Request) {
	ps, err := s.svc.Platforms(r.Context())
	reply(w, http.StatusOK, ps, err)
}

// connectPlatform opens the login window on the user's own desktop and
// returns at once (202); the outcome arrives via the activity feed and live
// updates. Only the token-holding board can call this, so a web page can't
// pop windows on the user's screen.
func (s *server) connectPlatform(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.StartConnect(r.PathValue("name"))
	if errors.Is(err, service.ErrLoginInProgress) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	reply(w, http.StatusAccepted, st, err)
}

// syncPlatform reads the user's application history on the site (TopCV only
// for now) and updates the board.
func (s *server) syncPlatform(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("name") != "topcv" {
		writeErr(w, http.StatusBadRequest, errors.New("status sync is available for TopCV only"))
		return
	}
	res, err := s.svc.SyncTopCV(context.WithoutCancel(r.Context()))
	reply(w, http.StatusOK, res, err)
}

func (s *server) disconnectPlatform(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.svc.Disconnect(r.Context(), r.PathValue("name"))
	reply(w, http.StatusOK, map[string]bool{"deleted": deleted}, err)
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
	case errors.Is(err, service.ErrProfileEmpty):
		writeErr(w, http.StatusConflict, err) // a precondition: fill in the profile first
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
