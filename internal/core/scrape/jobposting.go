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

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
)

// JobPosting is the subset of schema.org/JobPosting the board needs. Empty
// fields mean "not present / not parseable" — never a guess.
type JobPosting struct {
	Title       string
	Company     string
	Location    string
	Salary      string
	Skills      []string
	Description string // plain-text JD (HTML tags stripped)
	Email       string // contact email, when one is discoverable
	Deadline    string // validThrough, as the ISO date the site emitted
}

var (
	ldBlockRE = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRE      = regexp.MustCompile(`\s+`)
	emailRE   = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	digitRE   = regexp.MustCompile(`[0-9]`)
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
	return parsePasted(text, true)
}

// ParsePost is ParsePastedJD for a social-media post: the company is taken only
// from a phrase that names one, never guessed from the second line (a post's
// second line is usually a location or an emoji bullet, not an employer).
func ParsePost(text string) JobPosting {
	return parsePasted(text, false)
}

func parsePasted(text string, lineTwoIsCompany bool) JobPosting {
	phrases := split(text, phraseBreakRE)
	jp := JobPosting{Description: cleanText(text)}
	if len(phrases) > 0 {
		jp.Title = truncate(phrases[0], 120)
	}
	jp.Company = pastedCompany(text, phrases, lineTwoIsCompany)
	jp.Email = emailRE.FindString(text)
	// Salary is read per sentence, not per phrase, so a spaced range
	// ("$1,500 - $2,000") isn't cut at its dash.
	jp.Salary = pastedSalary(split(text, sentenceBreakRE))
	return jp
}

// Pasted text is split per line, then per sentence, and for title/company
// also at spaced separators ("A - B", "A | B", "• B"). That way a JD pasted as
// one line (common from agents and chat apps) still yields a title, company and
// salary instead of one giant title. An unspaced "15-20 triệu" stays whole.
var (
	sentenceBreakRE = regexp.MustCompile(`[.!?;]\s+`)
	phraseBreakRE   = regexp.MustCompile(`[.!?;]\s+|\s+[-–—|•]\s+|^\s*[-•]\s+`)
)

// split returns the non-empty pieces of text, line by line, cut at brk.
func split(text string, brk *regexp.Regexp) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for _, seg := range brk.Split(line, -1) {
			if seg = strings.TrimSpace(strings.TrimRight(seg, ".")); seg != "" {
				out = append(out, seg)
			}
		}
	}
	return out
}

// companyRE spots a phrase naming an employer (Vietnamese or English forms).
var companyRE = regexp.MustCompile(`(?i)(^|\s)(công ty|cty|tập đoàn|company|corporation|corp|jsc|ltd|llc|inc|tnhh)(\s|$|\.|,)`)

// pastedCompany finds the employer: the first phrase after the title that looks
// like a company name, else (multi-line pastes) the second line, as before.
// Salary, location, skill and requirement lines are never an employer.
func pastedCompany(text string, segs []string, lineTwoIsCompany bool) string {
	for i, seg := range segs {
		if i > 0 && companyRE.MatchString(seg) && len(seg) <= 120 {
			return strings.TrimSpace(labelRE.ReplaceAllString(seg, ""))
		}
	}
	if !lineTwoIsCompany {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 1 && !notCompany(lines[1]) {
		return truncate(lines[1], 120)
	}
	return ""
}

// labelRE strips a leading "Label:" ("Mức lương:", "Company:") from a phrase.
var labelRE = regexp.MustCompile(`^[^:]{1,20}:\s*`)

// notCompany reports whether a pasted line is pay, place, requirements or
// perks instead of an employer. The second line of a JD is often one of these,
// so it must never become the company. A name that merely mentions a place
// ("Shopee Vietnam", "VNG - Hồ Chí Minh") is still a company.
func notCompany(line string) bool {
	return detailLabelRE.MatchString(line) || payLineRE.MatchString(line) || isPlaceLine(line)
}

// detailLabelRE spots a line that starts with a JD detail label.
var detailLabelRE = regexp.MustCompile(`(?i)^(mức lương|lương|salary|thu nhập|compensation|thỏa thuận|negotiable|địa điểm|địa chỉ|location|nơi làm việc|workplace|hybrid|remote|onsite|on-site|yêu cầu|requirements?|qualifications?|kỹ năng|skills?|kinh nghiệm|experience|mô tả|description|quyền lợi|benefits?|phúc lợi|hạn nộp|deadline|liên hệ|contact|email)(\s|$|:|,|\.|-)`)

// payLineRE spots a line that starts with an amount of pay ("30-40 triệu",
// "$1,500", "Up to 2000 USD").
var payLineRE = regexp.MustCompile(`(?i)^(up to|upto|từ|tới|đến)?\s*(\$\s*\d|\d[\d.,]*\s*(-|–|~|đến|to)?\s*[\d.,]*\s*(triệu|tr|million|usd|vnd|đ|\$)(\s|$|/|,|\.|\)))`)

// isPlaceLine reports whether every part of a line is a place ("Hà Nội",
// "Quận 1, TP. Hồ Chí Minh", "Ho Chi Minh City, Vietnam").
func isPlaceLine(line string) bool {
	found := false
	for _, part := range placeSepRE.Split(strings.ToLower(line), -1) {
		part = strings.TrimSpace(placePrefixRE.ReplaceAllString(strings.TrimSpace(part), ""))
		if part == "" {
			continue
		}
		if !places[part] && !districtRE.MatchString(part) {
			return false
		}
		found = true
	}
	return found
}

var (
	placeSepRE    = regexp.MustCompile(`\s*[,|/–]\s*|\s+-\s+`)
	placePrefixRE = regexp.MustCompile(`^(tp\.?|thành phố|tỉnh|city of)\s*`)
	districtRE    = regexp.MustCompile(`^((quận|q\.|district)\s*\d+|(quận|huyện)\s+\S.*)$`)
	places        = map[string]bool{
		"hà nội": true, "ha noi": true, "hanoi": true, "hồ chí minh": true, "ho chi minh": true,
		"ho chi minh city": true, "hcm": true, "hcmc": true, "sài gòn": true, "saigon": true,
		"đà nẵng": true, "da nang": true, "danang": true, "hải phòng": true, "hai phong": true,
		"cần thơ": true, "can tho": true, "việt nam": true, "vietnam": true, "viet nam": true,
	}
)

// FetchJobPosting fetches a listing and extracts its JobPosting. It tries plain
// HTTP first (cheap, no browser); if that finds no JobPosting AND an external
// browser fetcher is configured (BRAVEDUCK_BROWSER_FETCHER), it escalates once to
// that fetcher for Cloudflare/JS-gated pages like TopCV. ok is false when neither
// tier finds a JobPosting — the caller still adds the job from the bare URL. Both
// tiers are best-effort; the only error is ErrNonPublicURL (or a resolve
// failure) for a link that must not be fetched at all — see CheckPublicURL.
// The browser tier can't use publicClient, so this up-front check is what keeps
// it off localhost/LAN.
func FetchJobPosting(ctx context.Context, u string) (JobPosting, bool, error) {
	if err := CheckPublicURL(ctx, u); err != nil {
		return JobPosting{}, false, err
	}
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

	resp, err := publicClient.Do(req)
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

// browserFetchPage renders a JS/Cloudflare-gated page and returns its HTML. By
// default it uses braveduck's built-in stealth browser (CloakBrowser Chromium,
// self-provisioned on first use) so the board works out of the box with no extra
// setup. Power users can override that with BRAVEDUCK_BROWSER_FETCHER — an external
// command run via `sh -c`, with the target URL passed in BRAVEDUCK_FETCH_URL (not
// string-interpolated, so no shell-injection surface) — that prints the rendered
// HTML to stdout. Returns "" on any failure, degrading to a bare-URL add.
func browserFetchPage(ctx context.Context, u string) string {
	if cmdStr := strings.TrimSpace(os.Getenv("BRAVEDUCK_BROWSER_FETCHER")); cmdStr != "" {
		return externalFetch(ctx, cmdStr, u)
	}
	html, err := browser.Fetch(ctx, u)
	if err != nil {
		return ""
	}
	return html
}

// externalFetch runs the BRAVEDUCK_BROWSER_FETCHER override command.
func externalFetch(ctx context.Context, cmdStr, u string) string {
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
	// A word instead of a figure ("Thương lượng", "Negotiable") takes no
	// currency or period.
	if !digitRE.MatchString(out) {
		return out
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

// skillSepRE splits a skills string ("Go, SQL; Docker").
var skillSepRE = regexp.MustCompile(`[,\n;]`)

// skills reads the skills field: a comma/newline string or an array of strings.
func skills(v any) []string {
	var raw []string
	switch t := v.(type) {
	case string:
		raw = skillSepRE.Split(t, -1)
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
var salaryRE = regexp.MustCompile(`(?i)(salary|lương|\$|usd|vnd|triệu|million)`)

// pastedSalary returns the first short phrase that mentions pay, minus any
// "Mức lương:" style label (with or without a colon).
func pastedSalary(segs []string) string {
	for _, seg := range segs {
		if salaryRE.MatchString(seg) && len(seg) < 80 {
			v := strings.TrimSpace(labelRE.ReplaceAllString(seg, ""))
			v = strings.TrimSpace(salaryLabelRE.ReplaceAllString(v, ""))
			if v != "" {
				return truncate(v, 80)
			}
			return truncate(seg, 80)
		}
	}
	return ""
}

// salaryLabelRE drops a bare pay label such as "Lương " or "Salary -".
var salaryLabelRE = regexp.MustCompile(`(?i)^(mức\s+)?(lương|salary|thu nhập|pay)\s*[:\-]?\s*`)

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
