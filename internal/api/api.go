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
	"strconv"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/rating"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

//go:embed web
var webFS embed.FS

// now returns the current time; overridable in tests.
var now = time.Now

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
	s := &server{st: st}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.createJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("PATCH /api/jobs/{id}", s.patchJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.deleteJob)
	mux.HandleFunc("POST /api/jobs/{id}/pin", s.togglePin)
	mux.HandleFunc("POST /api/jobs/{id}/keep", s.toggleKeep)
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

type server struct{ st *store.Store }

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	f := store.ListFilter{
		Status: r.URL.Query().Get("status"),
		Source: r.URL.Query().Get("source"),
	}
	list, err := s.st.List(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []jobs.Job{}
	}
	writeJSON(w, http.StatusOK, list)
}

// createReq is the Extract & Add payload: a single pasted string that is either
// a listing URL or free-text JD.
type createReq struct {
	Content string `json:"content"`
}

// createJob is "Extract & Add", done without any AI. A pasted URL is fetched and
// its schema.org/JobPosting JSON-LD lifted verbatim; free text falls back to
// line/regex heuristics. Fields we can't read stay empty rather than guessed.
func (s *server) createJob(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	content := strings.TrimSpace(req.Content)
	if len(content) < 5 {
		writeErr(w, http.StatusBadRequest, errors.New("paste a job URL or description"))
		return
	}

	var jp scrape.JobPosting
	var srcURL, source string
	if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
		srcURL = content
		source = scrape.SourceFor(srcURL)
		dup, err := s.st.ExistsByURL(r.Context(), srcURL)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if dup {
			writeErr(w, http.StatusConflict, errors.New("this job is already on the board"))
			return
		}
		// Best-effort: a blocked/JS-only page just yields a bare-URL card. A link
		// to localhost/LAN is refused outright — we never fetch those.
		var fetchErr error
		jp, _, fetchErr = scrape.FetchJobPosting(r.Context(), srcURL)
		if errors.Is(fetchErr, scrape.ErrNonPublicURL) {
			writeErr(w, http.StatusBadRequest, fetchErr)
			return
		}
	} else {
		jp = scrape.ParsePastedJD(content)
		source = "paste"
	}

	title := jp.Title
	if title == "" {
		title = "Untitled listing"
	}
	j := jobs.Job{
		Title:           title,
		Company:         jp.Company,
		URL:             srcURL,
		Location:        jp.Location,
		Status:          "saved",
		Salary:          jp.Salary,
		Skills:          strings.Join(jp.Skills, ", "),
		FullJD:          jp.Description,
		ContactEmail:    jp.Email,
		DiscoverySource: source,
	}
	if jp.Deadline != "" {
		j.ExpiresAt = jobs.ExpirePrefix + jp.Deadline
	}

	id, err := s.st.Add(r.Context(), j, now())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	saved, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.logEvent(r.Context(), "added", fmt.Sprintf("Added %q", saved.Title))
	writeJSON(w, http.StatusCreated, saved)
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	j, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// jobPatch is the mutable subset a board edits: lane moves and the two toggles.
// Pointers distinguish "field omitted" from a zero value, so a PATCH touches
// only what it names.
type jobPatch struct {
	Status *string `json:"status"`
	Pinned *bool   `json:"pinned"`
	Keep   *bool   `json:"keep"`
}

func (s *server) patchJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var p jobPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	j, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	moved := p.Status != nil && *p.Status != j.Status
	if p.Status != nil {
		j.Status = *p.Status
	}
	if p.Pinned != nil {
		j.Pinned = *p.Pinned
	}
	if p.Keep != nil {
		j.Keep = *p.Keep
	}
	if err := s.st.Update(r.Context(), j, now()); err != nil {
		writeStoreErr(w, err)
		return
	}
	if moved {
		s.logEvent(r.Context(), "moved", fmt.Sprintf("Moved %q to %s", j.Title, j.Status))
	}
	// Re-read so the caller gets the persisted row (incl. bumped updated_at).
	j, err = s.st.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *server) deleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	// Read the title first so the activity line is meaningful; absent job is fine.
	j, _ := s.st.Get(r.Context(), id)
	if err := s.st.Delete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	if j.Title != "" {
		s.logEvent(r.Context(), "removed", fmt.Sprintf("Removed %q", j.Title))
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// togglePin and toggleKeep flip one flag and return the updated job; they are
// the §1.5 contract's POST /pin and /keep, which the board's buttons call.
func (s *server) togglePin(w http.ResponseWriter, r *http.Request) {
	s.toggle(w, r, func(j *jobs.Job) { j.Pinned = !j.Pinned })
}

func (s *server) toggleKeep(w http.ResponseWriter, r *http.Request) {
	s.toggle(w, r, func(j *jobs.Job) { j.Keep = !j.Keep })
}

func (s *server) toggle(w http.ResponseWriter, r *http.Request, flip func(*jobs.Job)) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	j, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	flip(&j)
	if err := s.st.Update(r.Context(), j, now()); err != nil {
		writeStoreErr(w, err)
		return
	}
	j, err = s.st.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
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

// getRatingConfig returns the stored A/B rater config (normalized, so a fresh
// DB yields the single default rater).
func (s *server) getRatingConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetRatingConfig(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toRatingJSON(c))
}

// putRatingConfig validates strictly (see rating.Config.Validate) and saves.
// An invalid config is a 400 with the reason, never a silent repair.
func (s *server) putRatingConfig(w http.ResponseWriter, r *http.Request) {
	var in ratingConfigJSON
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cfg := rating.Config{ABEnabled: in.ABMode, Models: in.Models, DefaultModel: in.DefaultModel}
	if err := cfg.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c, err := s.st.SetRatingConfig(r.Context(), cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toRatingJSON(c))
}

// getCrawlStatus reports per-source crawl health and today's find count. The
// source rows come from crawl_runs (empty until a crawler is ported); the
// today-count is real from the jobs table regardless.
func (s *server) getCrawlStatus(w http.ResponseWriter, r *http.Request) {
	view, err := s.st.CrawlStatus(r.Context(), now())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// getActivity returns the recent activity feed (server-backed, so it survives a
// browser change and captures every client's actions). Newest first, capped.
func (s *server) getActivity(w http.ResponseWriter, r *http.Request) {
	events, err := s.st.RecentEvents(r.Context(), 20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// retryCrawl is an honest not-implemented stub: crawlers aren't ported into
// braveduck-cli yet (they need the captured-session/browser layer), so there's
// no run to re-trigger. Returns 501 with a clear message rather than faking
// success, so the board can tell the user the truth.
func (s *server) retryCrawl(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"ok":      false,
		"source":  source,
		"message": "crawlers aren't wired into braveduck-cli yet — nothing to retry",
	})
}

// logEvent records an activity-feed entry best-effort: a logging failure must
// never fail the mutation that triggered it, so the error is swallowed. A
// cancelled request context is ignored too.
func (s *server) logEvent(ctx context.Context, typ, message string) {
	if ctx.Err() != nil {
		ctx = context.Background()
	}
	_ = s.st.LogEvent(ctx, typ, message, now())
}

func (s *server) getKeywords(w http.ResponseWriter, r *http.Request) {
	sets, err := s.st.AllKeywords(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sets)
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

func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if store.IsNotFound(err) {
		writeErr(w, http.StatusNotFound, errors.New("no such job"))
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
