// Package api is the HTTP face over the shared core and store: a small JSON
// REST surface plus the embedded web board. It's what `braveduck serve` boots,
// and the contract the (separately built, then vendored) front-end talks to.
//
// The router is stdlib net/http with Go 1.22 method+path patterns — no
// framework, matching the CLI's dependency-free stance. Every handler is a thin
// translation between JSON and the store; all real logic lives in the core.
package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

//go:embed web
var webFS embed.FS

// now returns the current time; overridable in tests.
var now = time.Now

// Handler builds the http.Handler serving the API and the embedded board.
func Handler(st *store.Store) http.Handler {
	s := &server{st: st}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("PATCH /api/jobs/{id}", s.patchJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.deleteJob)
	mux.HandleFunc("GET /api/keywords", s.getKeywords)
	mux.HandleFunc("GET /api/rating-config", s.getRatingConfig)
	mux.HandleFunc("GET /api/crawl-status", s.getCrawlStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Everything else: the embedded web board (index.html + assets).
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
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
	if err := s.st.Delete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// getRatingConfig is a placeholder the board awaits at boot; it returns an inert
// shape until the scoring subsystem is ported — see docs/web-board-brief.md.
func (s *server) getRatingConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"default_model": "",
		"ab_mode":       false,
		"models":        []string{},
	})
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
