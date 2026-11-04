package crawl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const clSearchPage = `<ul>
<li data-testid="job-result-row"><a title="AI Developer" href="/tim-viec-lam/ai-developer/3655895"><h2>AI Developer</h2></a></li>
<li data-testid="job-result-row"><a href="/tim-viec-lam/ai-developer/3655895">again</a></li>
<li data-testid="job-result-row"><a href="/tim-viec-lam/growth-marketing-executive/3648824">Growth</a></li>
<li data-testid="job-result-row"><a href="/tim-viec-lam/broken-posting/1">Broken</a></li>
<li data-testid="job-result-row"><a href="/tim-viec-lam/one-too-many/2">Over the cap</a></li>
<a href="/viec-lam-cua/apec-group/20444">company page</a>
</ul>`

func clDetail(title, company string) string {
	return fmt.Sprintf(`<html><script type="application/ld+json">{"@context":"https://schema.org","@type":"JobPosting",
"title":%q,"hiringOrganization":{"@type":"Organization","name":%q},
"jobLocation":{"@type":"Place","address":{"@type":"PostalAddress","addressLocality":"Hà Nội"}},
"baseSalary":{"@type":"MonetaryAmount","currency":"VND","value":{"@type":"QuantitativeValue","minValue":15000000,"maxValue":25000000,"unitText":"MONTH"}},
"description":"<p>Xây dựng workflow n8n.</p><p>Gửi CV về hr@apec.vn</p>","validThrough":"2026-12-01T00:00:00+07:00"}</script></html>`, title, company)
}

func TestCareerLinkSearch(t *testing.T) {
	var gotQuery string
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/vieclam/list":
			gotQuery = r.URL.Query().Get("keyword")
			fmt.Fprint(w, clSearchPage)
		case "/tim-viec-lam/ai-developer/3655895":
			fmt.Fprint(w, clDetail("AI Developer", "APEC Group"))
		case "/tim-viec-lam/growth-marketing-executive/3648824":
			fmt.Fprint(w, clDetail("Growth Marketing Executive", "Duck Co"))
		case "/tim-viec-lam/broken-posting/1":
			http.Error(w, "gone", http.StatusNotFound)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &CareerLink{Base: srv.URL, Client: srv.Client(), PerQuery: 3}
	ls, err := c.Search(context.Background(), "ai automation")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "ai automation" {
		t.Fatalf("searched for %q", gotQuery)
	}
	// The broken posting is skipped; the 4th link is past PerQuery and never fetched.
	if len(ls) != 2 || strings.Contains(strings.Join(requests, " "), "one-too-many") {
		t.Fatalf("listings = %+v, requests = %v", ls, requests)
	}
	l := ls[0]
	if l.Title != "AI Developer" || l.Company != "APEC Group" || l.URL != "https://www.careerlink.vn/tim-viec-lam/ai-developer/3655895" ||
		l.Location != "Hà Nội" || l.Salary != "15000000 - 25000000 VND/month" || l.Deadline != "2026-12-01" ||
		l.Email != "hr@apec.vn" || !strings.Contains(l.Description, "workflow n8n") {
		t.Fatalf("listing = %+v", l)
	}
}

func TestCareerLinkSearchPageFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer srv.Close()
	c := &CareerLink{Base: srv.URL, Client: srv.Client(), PerQuery: 3}
	if _, err := c.Search(context.Background(), "n8n"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want the search page's HTTP 403", err)
	}
}
