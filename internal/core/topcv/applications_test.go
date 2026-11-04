package topcv

import "testing"

// Shaped like ExtractJS output from the live page (values made up).
const fixture = `[
 {"jobId":"2193898","title":"Senior Marketing Executive","company":"AWING","url":"https://www.topcv.vn/viec-lam/senior-marketing-executive/2193898.html",
  "appliedAt":"2026-06-15 21:34:01","viewed":false,"statusText":"TopCV đã nhắc NTD xem và phản hồi CV của bạn","statusTime":"22-06-2026"},
 {"jobId":"2190001","title":"MarTech Executive","company":"VUS","url":"https://www.topcv.vn/brand/vus-etsc/tuyen-dung/martech-executive-j2190001.html",
  "appliedAt":"2026-06-15 11:48:00","viewed":true,"statusText":"NTD đã xem CV. TopCV đã nhắc NTD phản hồi","statusTime":"22-06-2026"},
 {"jobId":"2190002","title":"AI Content Operator","company":"X","url":"","appliedAt":"","viewed":true,"statusText":"CV phù hợp","statusTime":""},
 {"jobId":"2190003","title":"Designer","company":"Y","url":"","appliedAt":"","viewed":true,"statusText":"CV chưa phù hợp / không phù hợp","statusTime":""},
 {"jobId":"","title":"broken card","company":"","url":""}
]`

func TestParseAndStatus(t *testing.T) {
	apps, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 4 {
		t.Fatalf("got %d apps, want 4 (id-less card dropped)", len(apps))
	}
	want := []string{StatusSent, StatusSeen, StatusSuitable, StatusRejected}
	for i, a := range apps {
		if a.Status() != want[i] {
			t.Errorf("%s: status %q, want %q", a.Title, a.Status(), want[i])
		}
	}
	if apps[0].AppliedDate() != "2026-06-15" || apps[2].AppliedDate() != "" {
		t.Fatalf("applied dates = %q %q", apps[0].AppliedDate(), apps[2].AppliedDate())
	}
	if _, err := Parse("not json"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestJobIDFromURL(t *testing.T) {
	for u, want := range map[string]string{
		"https://www.topcv.vn/viec-lam/senior-marketing-executive/2193898.html":          "2193898",
		"https://www.topcv.vn/brand/vus-etsc/tuyen-dung/martech-executive-j2190001.html": "2190001",
		"https://www.vietnamworks.com/x-2193898.html":                                    "",
		"https://www.topcv.vn/viec-lam":                                                  "",
	} {
		if got := JobIDFromURL(u); got != want {
			t.Errorf("JobIDFromURL(%q) = %q, want %q", u, got, want)
		}
	}
}
