package crawl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A response shaped like the live API (2026-11-04): numeric jobId, HTML
// description, geoLoc 0/0 for "unknown", RFC3339 expiry with +07:00.
const vnwFixture = `{"meta":{"nbHits":2},"data":[
 {"jobId":2127630,"jobTitle":"AI Automation Engineer","companyName":"Duck Co",
  "jobUrl":"https://www.vietnamworks.com/ai-automation-engineer-2127630-jv","prettySalary":"$1,500 - $2,500",
  "expiredOn":"2026-12-04T23:59:59+07:00",
  "jobDescription":"<p>Build <b>n8n</b> flows &amp; agents</p><ul><li>Python</li><li>LLMs</li></ul>",
  "jobRequirement":"<p>3+ years</p>","emailAddress":"hr@duck.vn",
  "skills":[{"skillName":"n8n"},{"skillName":"Python"}],
  "workingLocations":[{"address":"10 Mai Chí Thọ","cityName":"Ho Chi Minh","geoLoc":{"lat":10.77,"lon":106.72}}]},
 {"jobId":2127631,"jobTitle":"Kế toán","companyName":"X","jobUrl":"https://www.vietnamworks.com/ke-toan-2127631-jv",
  "workingLocations":[{"address":"Ha Noi","cityName":"Ha Noi","geoLoc":{"lat":0,"lon":0}}]},
 {"jobId":1,"jobTitle":"","jobUrl":""}
]}`

func TestVietnamWorksSearch(t *testing.T) {
	var got map[string]any
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST (GET is a 404 on the real API)", r.Method)
		}
		ua = r.Header.Get("User-Agent")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(vnwFixture))
	}))
	defer srv.Close()

	v := NewVietnamWorks()
	v.Endpoint = srv.URL
	ls, err := v.Search(context.Background(), "automation")
	if err != nil {
		t.Fatal(err)
	}

	if got["query"] != "automation" || got["page"] != float64(0) || got["userId"] != float64(0) {
		t.Fatalf("request body = %v", got)
	}
	if _, isArray := got["ranges"].([]any); !isArray {
		t.Fatal("ranges must be an array (empty), not null")
	}
	if !strings.HasPrefix(ua, "Mozilla/5.0") {
		t.Fatalf("user agent %q would be refused by nginx", ua)
	}

	if len(ls) != 2 {
		t.Fatalf("got %d listings, want 2 (the title-less hit skipped)", len(ls))
	}
	a := ls[0]
	if a.Title != "AI Automation Engineer" || a.Company != "Duck Co" || a.Salary != "$1,500 - $2,500" ||
		a.Deadline != "2026-12-04" || a.Email != "hr@duck.vn" || a.Location != "10 Mai Chí Thọ, Ho Chi Minh" {
		t.Fatalf("mapped wrong: %+v", a)
	}
	if !a.HasGeo || a.Lat != 10.77 || a.Lng != 106.72 {
		t.Fatalf("geo = %v %v %v", a.HasGeo, a.Lat, a.Lng)
	}
	if strings.Contains(a.Description, "<") || !strings.Contains(a.Description, "n8n flows & agents") ||
		!strings.Contains(a.Description, "• Python") || !strings.Contains(a.Description, "3+ years") {
		t.Fatalf("description = %q", a.Description)
	}
	if strings.Join(a.Skills, ",") != "n8n,Python" {
		t.Fatalf("skills = %v", a.Skills)
	}
	if ls[1].HasGeo {
		t.Fatal("geoLoc 0/0 means unknown, not the Gulf of Guinea")
	}
}

func TestVietnamWorksErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"403":         func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "forbidden", 403) },
		"not json":    func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("<html>maintenance</html>")) },
		"wrong shape": func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"data":{"oops":1}}`)) },
	} {
		srv := httptest.NewServer(h)
		v := NewVietnamWorks()
		v.Endpoint = srv.URL
		if _, err := v.Search(context.Background(), "x"); err == nil {
			t.Errorf("%s: no error", name)
		}
		srv.Close()
	}
}
