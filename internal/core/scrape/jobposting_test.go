package scrape

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func ld(json string) string {
	return `<html><script type="application/ld+json">` + json + `</script></html>`
}

// ExtractJobPosting reads the shapes real job sites emit.
func TestExtractJobPostingShapes(t *testing.T) {
	for _, tc := range []struct {
		name, page                                         string
		title, company, location, salary, deadline, skills string
	}{
		{"@graph wrapper, @type list",
			ld(`{"@context":"https://schema.org","@graph":[{"@type":"Organization","name":"x"},{"@type":["JobPosting"],"title":"Data Analyst","hiringOrganization":"MoMo"}]}`),
			"Data Analyst", "MoMo", "", "", "", ""},
		{"top-level array, region only, from-salary",
			ld(`[{"@type":"WebPage"},{"@type":"JobPosting","title":"Kế toán","hiringOrganization":{"name":"Duck &amp; Co"},
				"jobLocation":[{"address":{"addressRegion":"Hồ Chí Minh"}},{"address":{"addressLocality":"Hà Nội"}}],
				"baseSalary":{"currency":"VND","value":{"minValue":"15000000","unitText":"MONTH"}}}]`),
			"Kế toán", "Duck & Co", "Hồ Chí Minh", "from 15000000 VND/month", "", ""},
		{"locality and region, up-to salary, skills list, RFC3339 deadline",
			ld(`{"@type":"JobPosting","title":"Dev","jobLocation":{"address":{"addressLocality":"Cầu Giấy","addressRegion":"Hà Nội"}},
				"baseSalary":{"currency":"USD","value":{"maxValue":2000.5}},"skills":["Go"," SQL ",""],"validThrough":"2026-12-01T00:00:00+07:00"}`),
			"Dev", "", "Cầu Giấy, Hà Nội", "up to 2000.5 USD", "2026-12-01", "Go|SQL"},
		{"single value, skills string, bad deadline",
			ld(`{"@type":"JobPosting","title":"QA","baseSalary":{"value":{"value":12000000}},"skills":"Go, SQL; Docker","validThrough":"soon"}`),
			"QA", "", "", "12000000", "", "Go|SQL|Docker"},
		{"broken JSON first, a good block after",
			`<script type="application/ld+json">{oops</script>` + ld(`{"@type":"JobPosting","title":"Second"}`),
			"Second", "", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jp, ok := ExtractJobPosting(tc.page)
			if !ok {
				t.Fatal("no JobPosting found")
			}
			if jp.Title != tc.title || jp.Company != tc.company || jp.Location != tc.location || jp.Salary != tc.salary ||
				jp.Deadline != tc.deadline || strings.Join(jp.Skills, "|") != tc.skills {
				t.Errorf("got %+v", jp)
			}
		})
	}
	if _, ok := ExtractJobPosting(ld(`{"@type":"Organization","name":"x"}`)); ok {
		t.Error("a page without a JobPosting matched")
	}
}

func TestSourceFor(t *testing.T) {
	for u, want := range map[string]string{
		"https://www.topcv.vn/viec-lam/x/1.html":   "topcv",
		"https://www.vietnamworks.com/x-1-jv":      "vietnamworks",
		"https://vn.joboko.com/viec-lam-x-xvi1":    "joboko",
		"https://www.careerlink.vn/tim-viec-lam/1": "careerlink",
		"https://www.linkedin.com/jobs/view/1":     "linkedin",
		"https://jobs.example.com/1":               "jobs.example.com",
		"":                                         "paste",
		"::not a url":                              "paste",
	} {
		if got := SourceFor(u); got != want {
			t.Errorf("SourceFor(%q) = %q, want %q", u, got, want)
		}
	}
	if got := SourceNames(); !strings.HasSuffix(got, " or Facebook") || !strings.HasPrefix(got, "TopCV, ") {
		t.Errorf("SourceNames() = %q", got)
	}
}

// A link to this machine or the LAN is refused before anything is fetched.
func TestFetchJobPostingRefusesNonPublic(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:6379/", "http://192.168.1.1/admin", "file:///etc/passwd"} {
		if _, _, err := FetchJobPosting(context.Background(), u); !errors.Is(err, ErrNonPublicURL) {
			t.Errorf("FetchJobPosting(%q) = %v, want ErrNonPublicURL", u, err)
		}
	}
}

// The BRAVEDUCK_BROWSER_FETCHER override gets the URL in its environment
// (never interpolated into the command) and its stdout is the page.
func TestExternalFetch(t *testing.T) {
	page := externalFetch(context.Background(), `printf '<p>%s</p>' "$BRAVEDUCK_FETCH_URL"`, "https://x.vn/a?b=1;rm -rf /")
	if page != "<p>https://x.vn/a?b=1;rm -rf /</p>" {
		t.Fatalf("page = %q", page)
	}
	if got := externalFetch(context.Background(), "exit 3", "https://x.vn"); got != "" {
		t.Fatalf("failed command = %q, want empty", got)
	}
}
