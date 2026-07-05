// Package scrape ports the public detail-page scraping logic — application
// deadlines and (for TopCV) salary — from the original app's jobExpiry.ts.
//
// The deadline extractor here is a faithful port of fetchExpiry: it pulls
// validThrough (joboko, careerlink JSON-LD) or expiredOn (VietnamWorks' escaped
// RSC stream) out of a listing page's HTML. The pure ExtractDeadline function is
// separated from the HTTP FetchDeadline wrapper so the extraction is testable
// against fixtures with no network.
//
// TopCV is deliberately NOT handled here: its page 403s plain HTTP (Cloudflare)
// and injects JSON-LD via JS, so it needs a real browser (chromedp) — see the
// apply/session packages. That path is intentionally left as a TODO stub below.
package scrape

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120 Safari/537.36"

var (
	// VNW streams the field backslash-escaped (\"expiredOn\":\"...\"); we unescape
	// first so one pair of regexes covers plain JSON-LD and the RSC payload.
	validThroughRE = regexp.MustCompile(`"validThrough"\s*:\s*"([^"]+)"`)
	expiredOnRE    = regexp.MustCompile(`"expiredOn"\s*:\s*"([^"]+)"`)
)

// deadlineLayouts mirror what JS `new Date(iso)` accepts for the ISO values these
// sites emit (bare date or full offset datetime). If none parse, the value is
// treated as absent — same as the TS Number.isNaN guard.
var deadlineLayouts = []string{
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func validDate(s string) bool {
	for _, l := range deadlineLayouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	return false
}

// ExtractDeadline pulls the application deadline out of a listing page's raw
// HTML. Returns the ISO string (verbatim, as the site emitted it) or "" when no
// parseable validThrough/expiredOn field is present.
func ExtractDeadline(html string) string {
	// Unescape RSC-style \" so the JSON-LD regexes match either representation.
	text := strings.ReplaceAll(html, `\"`, `"`)
	m := validThroughRE.FindStringSubmatch(text)
	if m == nil {
		m = expiredOnRE.FindStringSubmatch(text)
	}
	if m == nil {
		return ""
	}
	iso := strings.TrimSpace(m[1])
	if !validDate(iso) {
		return ""
	}
	return iso
}

// FetchDeadline fetches a listing page over plain HTTP and extracts its deadline.
// Works for joboko / careerlink / vietnamworks. Returns "" (no error) when the
// page is blocked or has no deadline — the caller stores that as the "unknown"
// grammar value. TopCV must NOT be passed here (see FetchTopcvDetail).
func FetchDeadline(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "vi,en;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil // blocked / not found → unknown, not an error
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		return "", err
	}
	return ExtractDeadline(string(body)), nil
}

// TopcvDetail is the deadline + raw salary text scraped from a TopCV listing.
type TopcvDetail struct {
	Deadline string // ISO YYYY-MM-DD, or "" when blocked/absent
	Salary   string // raw "Mức lương" text, e.g. "8 - 16 triệu", or ""
}

// FetchTopcvDetail is a stub. TopCV's page is unreachable by plain HTTP
// (Cloudflare + JS-injected JSON-LD); in braveduck-cli it must be driven through
// a real browser via chromedp using the user's captured session. Wiring lives in
// the session/apply layer once that lands.
//
// TODO(chromedp): drive the TopCV detail page with the local browser, read
// "Hạn nộp hồ sơ"/"Hạn ứng tuyển" and "Mức lương", parse into TopcvDetail.
func FetchTopcvDetail(ctx context.Context, url string) (TopcvDetail, error) {
	return TopcvDetail{}, nil
}
