// Package topcv reads the user's own TopCV account pages (read-only) through a
// saved login session. See topcv-api-analysis docs/authenticated-api.md; the
// card structure below was observed on the live application-history page
// (2026-11-04).
package topcv

import (
	"encoding/json"
	"regexp"
	"strings"
)

// HistoryURL is the account page listing the user's applications.
const HistoryURL = "https://www.topcv.vn/lich-su-ung-tuyen"

// Application is one application card from the history page.
type Application struct {
	JobID      string `json:"jobId"` // data-job-id: TopCV's job id
	Title      string `json:"title"`
	Company    string `json:"company"`
	URL        string `json:"url"`        // the job's page (/viec-lam/… or /brand/…/tuyen-dung/…)
	AppliedAt  string `json:"appliedAt"`  // data-created-at, "YYYY-MM-DD HH:MM:SS" (Vietnam time)
	Viewed     bool   `json:"viewed"`     // data-viewed="1": the employer opened the CV
	StatusText string `json:"statusText"` // e.g. "NTD đã xem CV. TopCV đã nhắc NTD phản hồi…"
	StatusTime string `json:"statusTime"` // e.g. "22-06-2026"
}

// ExtractJS runs in the history page and returns its cards as JSON. It only
// reads the page; nothing is clicked.
const ExtractJS = `() => JSON.stringify([...document.querySelectorAll(".job-item[data-apply-id]")].map(c => {
  const t = c.querySelector("a.transform-job-title") || c.querySelector(".job-title a");
  const txt = sel => ((c.querySelector(sel) || {}).textContent || "").replace(/\s+/g, " ").trim();
  return {
    jobId: c.dataset.jobId || "",
    title: t ? t.textContent.replace(/\s+/g, " ").trim() : "",
    company: txt(".company-name"),
    url: t ? t.href.replace(/[?#].*$/, "") : "",
    appliedAt: c.dataset.createdAt || "",
    viewed: c.dataset.viewed === "1",
    statusText: txt(".cv-apply-status-text"),
    statusTime: txt(".cv-apply-status-time").replace(/[()]/g, ""),
  };
}))`

// Parse decodes ExtractJS output, dropping cards without a job id or title.
func Parse(raw string) ([]Application, error) {
	var all []Application
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		return nil, err
	}
	var out []Application
	for _, a := range all {
		if a.JobID != "" && a.Title != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

// Status codes stored in jobs.topcv_status (the board renders them).
const (
	StatusSent     = "sent"
	StatusSeen     = "seen"
	StatusSuitable = "suitable"
	StatusRejected = "rejected"
)

// Status condenses a card into one code. Rejection and "suitable" are read
// from the status line (TopCV's wording); otherwise the viewed flag decides.
func (a Application) Status() string {
	t := strings.ToLower(a.StatusText)
	switch {
	case strings.Contains(t, "không phù hợp") || strings.Contains(t, "từ chối"):
		return StatusRejected
	case strings.Contains(t, "phù hợp"):
		return StatusSuitable
	case a.Viewed || strings.Contains(t, "đã xem"):
		return StatusSeen
	default:
		return StatusSent
	}
}

// AppliedDate is the YYYY-MM-DD the application was sent, or "".
func (a Application) AppliedDate() string {
	if len(a.AppliedAt) >= 10 {
		return a.AppliedAt[:10]
	}
	return ""
}

var jobIDInURL = regexp.MustCompile(`(?:/|-j)(\d{5,})\.html`)

// JobIDFromURL extracts TopCV's job id from a listing URL (both the
// /viec-lam/<slug>/<id>.html and brand-page forms), or "".
func JobIDFromURL(u string) string {
	if !strings.Contains(u, "topcv.vn") {
		return ""
	}
	if m := jobIDInURL.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}
