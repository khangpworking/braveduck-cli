package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/markdown"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The board's AI buttons: braveduck writes nothing itself, so "rate", "tailor
// CV" and "draft" return the request to copy into the user's AI — a short
// one for an agent with braveduck connected, and a full brief for any chat.

type askOut struct {
	Prompt string `json:"prompt"` // for Claude Code etc. (braveduck over MCP)
	Brief  string `json:"brief"`  // paste-ready, for any AI chat
}

func (s *server) rateRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := s.svc.RateBrief(r.Context(), id)
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, askOut{service.AgentPrompt("rate", id, b.Jobs[0].Title), service.RenderRatingBrief(b)})
}

func (s *server) cvRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := s.svc.CVBrief(r.Context(), id)
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, askOut{service.AgentPrompt("cv", id, b.Job.Title), service.RenderDocBrief(b)})
}

func (s *server) draftRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Notes string `json:"notes"`
	}
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	b, err := s.svc.DraftBrief(r.Context(), id, in.Notes)
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, askOut{service.AgentPrompt("draft", id, b.Job.Title), service.RenderDocBrief(b)})
}

func (s *server) putCV(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Markdown string `json:"markdown"`
	}
	if !decode(w, r, &in) {
		return
	}
	j, err := s.svc.SubmitCV(r.Context(), id, in.Markdown)
	reply(w, http.StatusOK, j, err)
}

func (s *server) getCV(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	md, _, err := s.svc.CV(r.Context(), id)
	reply(w, http.StatusOK, map[string]string{"markdown": md, "html": markdown.ToHTML(md)}, err)
}

// renderPDF is swapped in tests (no browser needed).
var renderPDF = browser.RenderPDF

func (s *server) cvPDF(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	md, j, err := s.svc.CV(r.Context(), id)
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	pdf, err := renderPDF(context.WithoutCancel(r.Context()), markdown.Page(j.Title, markdown.ToHTML(md)))
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("couldn't make the PDF (browser unavailable): "+err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="cv-job-`+strconv.FormatInt(id, 10)+`.pdf"`)
	_, _ = w.Write(pdf)
}

func (s *server) putDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var d service.Draft
	if !decode(w, r, &d) {
		return
	}
	j, err := s.svc.SubmitDraft(r.Context(), id, d)
	reply(w, http.StatusOK, j, err)
}

func (s *server) draftSent(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		j, err := s.svc.MarkDraftSent(r.Context(), id)
		reply(w, http.StatusOK, j, err)
	}
}

func (s *server) draftReject(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		j, err := s.svc.RejectDraft(r.Context(), id)
		reply(w, http.StatusOK, j, err)
	}
}
