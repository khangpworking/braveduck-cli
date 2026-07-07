// JobPosting extraction: lift a listing's structured fields from its page so the
// board's "Extract & Add" can ingest a URL with no AI. Job sites embed a
// schema.org/JobPosting as <script type="application/ld+json">; that JSON is the
// exact, authoritative source (title, company, salary, location, skills,
// deadline), so we parse it rather than guess. Pasted free-text has no such
// structure, so ParsePastedJD falls back to line/regex heuristics.
//
// Everything here except FetchJobPosting is pure (HTML/text in, struct out) so it
// stays testable with no network.
package scrape

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// JobPosting is the subset of schema.org/JobPosting the board needs. Empty
// fields mean "not present / not parseable" — never a guess.
type JobPosting struct {
	Title       string
	Company     string
	Location    string
	Salary      string
	Skills      []string
	Description string   // plain-text JD (HTML tags stripped)
	Email       string   // contact email, when one is discoverable
	Deadline    string   // validThrough, as the ISO date the site emitted
}

var (
	ldBlockRE = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRE      = regexp.MustCompile(`\s+`)
	emailRE   = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

// ExtractJobPosting scans a page's ld+json blocks for a JobPosting node and lifts
// the fields the board needs. ok is false when no JobPosting is present.
func ExtractJobPosting(page string) (JobPosting, bool) {
	for _, m := range ldBlockRE.FindAllStringSubmatch(page, -1) {
		raw := strings.TrimSpace(m[1])
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			continue
		}
		for _, node := range flattenLD(v) {
			if isJobPosting(node) {
				return jobPostingFromNode(node), true
			}
		}
	}
	return JobPosting{}, false
}

// flattenLD walks a decoded ld+json value into its candidate object nodes,
// unwrapping top-level arrays and the @graph container.
func flattenLD(v any) []map[string]any {
	var out []map[string]any
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			out = append(out, flattenLD(e)...)
		}
	case map[string]any:
		out = append(out, t)
		if g, ok := t["@graph"]; ok {
			out = append(out, flattenLD(g)...)
		}
	}
	return out
}

func isJobPosting(node map[string]any) bool {
	switch t := node["@type"].(type) {
	case string:
		return t == "JobPosting"
	case []any:
		for _, e := range t {
			if s, _ := e.(string); s == "JobPosting" {
				return true
			}
		}
	}
	return false
}

func jobPostingFromNode(n map[string]any) JobPosting {
	jp := JobPosting{
		Title:       cleanText(str(n["title"])),
		Company:     cleanText(orgName(n["hiringOrganization"])),
		Location:    cleanText(locality(n["jobLocation"])),
		Salary:      cleanText(salary(n["baseSalary"])),
		Skills:      skills(n["skills"]),
		Description: cleanText(stripTags(str(n["description"]))),
		Deadline:    deadlineDate(str(n["validThrough"])),
	}
	if jp.Email == "" {
		jp.Email = emailRE.FindString(jp.Description)
	}
	return jp
}

// ParsePastedJD builds a best-effort JobPosting from free-text a user pasted:
// first non-empty line is the title, the second the company, plus a scan for a
// contact email and a salary line. Skills/description stay empty — no AI guess.
func ParsePastedJD(text string) JobPosting {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	jp := JobPosting{Description: cleanText(text)}
	if len(lines) > 0 {
		jp.Title = truncate(lines[0], 120)
	}
	if len(lines) > 1 {
		jp.Company = truncate(lines[1], 120)
	}
	jp.Email = emailRE.FindString(text)
	jp.Salary = pastedSalary(lines)
	return jp
}

// FetchJobPosting fetches a listing and extracts its JobPosting. It tries plain
// HTTP first (cheap, no browser); if that finds no JobPosting AND an external
// browser fetcher is configured (BRAVEDUCK_BROWSER_FETCHER), it escalates once to
// that fetcher for Cloudflare/JS-gated pages like TopCV. ok is false when neither
// tier finds a JobPosting — the caller still adds the job from the bare URL. Both
// tiers are best-effort, so the error is always nil (kept for signature stability).
func FetchJobPosting(ctx context.Context, u string) (JobPosting, bool, error) {
	if page := httpFetchPage(ctx, u); page != "" {
		if jp, ok := ExtractJobPosting(page); ok {
			return jp, true, nil
		}
	}
	if page := browserFetchPage(ctx, u); page != "" {
		if jp, ok := ExtractJobPosting(page); ok {
			return jp, true, nil
		}
	}
	return JobPosting{}, false, nil
}

// httpFetchPage GETs a page over plain HTTP, returning its body on 200 or ""
// on any error / non-200 (blocked, not found). Same request shape as FetchDeadline.
func httpFetchPage(ctx context.Context, u string) string {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "vi,en;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		return ""
	}
	return string(body)
}

// browserFetchPage runs the opt-in external browser fetcher for pages that block
// plain HTTP. The command comes from BRAVEDUCK_BROWSER_FETCHER (run via `sh -c`),
// with the target URL passed in BRAVEDUCK_FETCH_URL — not string-interpolated, so
// there's no shell-injection surface. The command must print the rendered HTML to
// stdout. Returns "" when unconfigured or on any failure, keeping braveduck a
// dependency-free binary that degrades to bare-URL adds. The browser stack
// (CloakBrowser, Playwright, …) lives entirely in that external script.
func browserFetchPage(ctx context.Context, u string) string {
	cmdStr := strings.TrimSpace(os.Getenv("BRAVEDUCK_BROWSER_FETCHER"))
	if cmdStr == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Env = append(os.Environ(), "BRAVEDUCK_FETCH_URL="+u)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// SourceFor maps a listing URL to the board's discovery_source tag.
func SourceFor(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "paste"
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.Contains(host, "topcv"):
		return "topcv"
	case strings.Contains(host, "vietnamworks"):
		return "vietnamworks"
	case strings.Contains(host, "joboko"):
		return "joboko"
	case strings.Contains(host, "careerlink"):
		return "careerlink"
	case strings.Contains(host, "linkedin"):
		return "linkedin"
	case host == "":
		return "paste"
	default:
		return host
	}
}

// --- field decoders (each tolerant of the shapes real pages emit) ---

func str(v any) string {
	s, _ := v.(string)
	return s
}

// orgName reads hiringOrganization, which is usually {name:...} but sometimes a
// bare string.
func orgName(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		return str(t["name"])
	}
	return ""
}

// locality reads jobLocation.address.addressLocality (possibly the first of an
// array of locations).
func locality(v any) string {
	switch t := v.(type) {
	case []any:
		if len(t) > 0 {
			return locality(t[0])
		}
	case map[string]any:
		if addr, ok := t["address"].(map[string]any); ok {
			loc := str(addr["addressLocality"])
			if reg := str(addr["addressRegion"]); reg != "" && reg != loc {
				if loc != "" {
					return loc + ", " + reg
				}
				return reg
			}
			return loc
		}
	}
	return ""
}

// salary formats baseSalary.value (a MonetaryAmount) as "min - max cur/unit".
func salary(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	cur := str(m["currency"])
	val, ok := m["value"].(map[string]any)
	if !ok {
		return ""
	}
	minV := num(val["minValue"])
	maxV := num(val["maxValue"])
	unit := str(val["unitText"])
	amount := num(val["value"])
	var out string
	switch {
	case minV != "" && maxV != "":
		out = minV + " - " + maxV
	case amount != "":
		out = amount
	case minV != "":
		out = "from " + minV
	case maxV != "":
		out = "up to " + maxV
	default:
		return ""
	}
	if cur != "" {
		out += " " + cur
	}
	if unit != "" {
		out += "/" + strings.ToLower(unit)
	}
	return out
}

// num renders a JSON number (float64 off encoding/json) or numeric string without
// a trailing ".0".
func num(v any) string {
	switch t := v.(type) {
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}

// skills reads the skills field: a comma/newline string or an array of strings.
func skills(v any) []string {
	var raw []string
	switch t := v.(type) {
	case string:
		for _, part := range regexp.MustCompile(`[,\n;]`).Split(t, -1) {
			raw = append(raw, part)
		}
	case []any:
		for _, e := range t {
			raw = append(raw, str(e))
		}
	}
	var out []string
	for _, s := range raw {
		if s = cleanText(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// deadlineDate normalizes validThrough to a bare YYYY-MM-DD, or "" if unparseable.
func deadlineDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, l := range deadlineLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// pastedSalary finds a line that looks like a salary quote in pasted text.
func pastedSalary(lines []string) string {
	sre := regexp.MustCompile(`(?i)(salary|lương|mức lương|\$|usd|vnd|triệu|million)`)
	for _, l := range lines {
		if sre.MatchString(l) && len(l) < 80 {
			return truncate(l, 80)
		}
	}
	return ""
}

func stripTags(s string) string { return tagRE.ReplaceAllString(s, " ") }

// cleanText unescapes HTML entities and collapses whitespace.
func cleanText(s string) string {
	s = html.UnescapeString(s)
	return strings.TrimSpace(wsRE.ReplaceAllString(s, " "))
}

func truncate(s string, n int) string {
	s = cleanText(s)
	if len(s) > n {
		return strings.TrimSpace(s[:n])
	}
	return s
}
