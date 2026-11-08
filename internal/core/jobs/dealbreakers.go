package jobs

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// Dealbreaker detection — port of textDealbreakers/geoDealbreakers from
// learning.tsx. Zoe's four hard dealbreakers, surfaced as pills so she never has
// to expand a card to catch them. Each appears only when detected; best-effort
// from data already on the row, recomputed per render (no new columns).
//
// TEXT dealbreakers (Saturday work / negotiable pay) come from the JD + salary
// captured at crawl and never change. GEO dealbreakers (too far / brand-new
// company) resolve async on pin. Labels are kept verbatim in Vietnamese.

var (
	// Saturday work. Precision-first: flag explicit 6-day / Saturday phrasing,
	// but suppress when a 5-day or "nghỉ thứ 7" (Saturday OFF) signal co-occurs.
	satOnRE  = regexp.MustCompile(`(?i)(6\s*ng[àa]y\s*/?\s*tu[ầa]n|6\s*days?\s*/?\s*(a\s*)?week|6[-\s]day|l[àa]m[^.]{0,20}(th[ứu]\s*7|th[ứu]\s*b[ảa]y)|th[ứu]\s*2[^.]{0,8}(-|đ[ếe]n|to)[^.]{0,8}th[ứu]\s*7|t2\s*-\s*t7|mon[a-z]*\s*[-–to]+\s*sat)`)
	satOffRE = regexp.MustCompile(`(?i)(5\s*ng[àa]y\s*/?\s*tu[ầa]n|5\s*days?\s*/?\s*(a\s*)?week|th[ứu]\s*2[^.]{0,8}(-|đ[ếe]n|to)[^.]{0,8}th[ứu]\s*6|t2\s*-\s*t6|mon[a-z]*\s*[-–to]+\s*fri|ngh[ỉi][^.]{0,15}(th[ứu]\s*7|th[ứu]\s*b[ảa]y|t7))`)
	// Explicit "AND Saturday" add-on ("Thứ 2 – Thứ 6 và sáng Thứ Bảy"). Overrides
	// satOff: a 5-day range PLUS extra Saturday work still means Saturday.
	satExtraRE = regexp.MustCompile(`(?i)(v[àa]|\+|k[èe]m|c[ộo]ng)\s*(s[áa]ng\s*|chi[ềe]u\s*)?(th[ứu]\s*7|th[ứu]\s*b[ảa]y|t7)`)
	// Negotiable / undisclosed pay (salary field only — JD-body mentions are noisy).
	negotiableRE = regexp.MustCompile(`(?i)th[ỏo]a?\s*thu[ậa]n|tho[ảa]\s*thu[ậa]n|th[ưuơo]{1,2}ng\s*l[ưuơợ]{1,2}ng|negotia|c[ạa]nh\s*tranh|competitive`)
)

var dateOnlyRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// TextDealbreakers returns the JD/salary-derived dealbreaker labels (never
// change once crawled).
func TextDealbreakers(j Job) []string {
	out := []string{}
	full := j.FullJD
	if full == "" {
		full = j.Summary
	}
	text := strings.ToLower(j.Title + " " + j.AISummary + " " + full)
	if satExtraRE.MatchString(text) || (satOnRE.MatchString(text) && !satOffRE.MatchString(text)) {
		out = append(out, "Làm T7")
	}
	if negotiableRE.MatchString(j.Salary) {
		out = append(out, "Lương thỏa thuận")
	}
	return out
}

const yearMS = 31557600000.0 // 365.25 days, matching the JS constant

// GeoDealbreakers returns the geo/company-derived dealbreaker labels (resolve
// async on pin). maxKM is the user's maximum commute (10 when unset).
func GeoDealbreakers(j Job, now time.Time, maxKM float64) []string {
	out := []string{}
	if maxKM <= 0 {
		maxKM = 10
	}
	// Beyond the maximum commute is the dealbreaker, but cap at 80km: a real
	// metro commute never exceeds that, so anything beyond is a wrong-province
	// geocode — suppress it rather than show a bogus "1131km".
	if j.CompanyGeoStatus == "ok" && j.HasDistance && j.CompanyDistanceKM > maxKM && j.CompanyDistanceKM <= 80 {
		out = append(out, "Xa "+itoa(int(math.Round(j.CompanyDistanceKM)))+"km")
	}
	if ad := j.CompanyActiveDate; dateOnlyRE.MatchString(ad) {
		if t, ok := parseTime(ad); ok {
			yrs := float64(now.Sub(t).Milliseconds()) / yearMS
			if yrs >= 0 && yrs < 2 {
				out = append(out, "Cty <2 năm")
			}
		}
	}
	if companyClosed(j.CompanyRegStatus) {
		out = append(out, "Cty ngừng hoạt động")
	}
	return out
}

// companyClosed reports whether the business register says the company has
// stopped operating ("NNT ngừng hoạt động và đã đóng MST", "NNT tạm ngừng
// hoạt động", …).
func companyClosed(status string) bool {
	s := strings.ToLower(status)
	return strings.Contains(s, "ngừng hoạt động") || strings.Contains(s, "đóng mst")
}

// Dealbreakers returns the combined text + geo dealbreaker labels for a job.
// The service puts them on every job it returns (Job.Dealbreakers), so the
// board, the CLI and MCP show the same ones.
func Dealbreakers(j Job, now time.Time, maxKM float64) []string {
	return append(TextDealbreakers(j), GeoDealbreakers(j, now, maxKM)...)
}

// itoa avoids pulling strconv for one integer format.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
