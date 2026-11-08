package company

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Pages shaped like the site's own (2026-11), cut down to what's read.
const searchPage = `<div class="tax-search-result-summary">Hiển thị 1–15 trong tổng số 212 doanh nghiệp</div>
<div class="tax-result-list">
  <div class="tax-result-item"><h3 class="tax-company-name">
    <a href="https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-cong-ty-tnhh-thuong-mai-san-xuat-thien-an.html" class="tax-company-link">
        CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN
    </a></h3></div>
  <div class="tax-result-item"><h3 class="tax-company-name">
    <a href="https://dulieuphapluat.vn/cong-cu/ma-so-thue/3701071364-cong-ty-tnhh-san-xuat-thuong-mai-an-thien.html" class="tax-company-link">
        CÔNG TY TNHH SẢN XUẤT THƯƠNG MẠI AN THIÊN
    </a></h3></div>
  <div class="tax-result-item"><h3 class="tax-company-name">
    <a href="https://dulieuphapluat.vn/cong-cu/ma-so-thue/0105320952-cong-ty-tnhh-thuong-mai-san-xuat-thien-an.html" class="tax-company-link">
        CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN AN
    </a></h3></div>
</div>`

const companyPage = `<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"DỮ LIỆU PHÁP LUẬT","url":"https://dulieuphapluat.vn"}</script>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization",
 "name":"CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN","legalName":"CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN",
 "url":"https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-cong-ty-tnhh-thuong-mai-san-xuat-thien-an.html",
 "taxID":"0309542960","foundingDate":"2009-10-20"}</script>
<div class="tax-detail-meta-item"><span class="tax-detail-meta-label">Trạng thái</span><span class="tax-detail-meta-chip "> NNT đã được cấp MST </span></div>
<tr><td class="tax-detail-label">Ngày bắt đầu hoạt động</td><td class="tax-detail-value">20/10/2009</td></tr>`

func site(t *testing.T, search, company string) (*Register, *atomic.Int32) {
	return siteWith(t, search, map[string]string{"0309542960": company})
}

// siteWith serves the search page and one company page per tax code.
func siteWith(t *testing.T, search string, pages map[string]string) (*Register, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/cong-cu/tra-cuu-ma-so-thue.html" {
			w.Write([]byte(search))
			return
		}
		for mst, page := range pages {
			if strings.HasPrefix(r.URL.Path, "/cong-cu/ma-so-thue/"+mst+"-") {
				w.Write([]byte(page))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ts.Close)
	reg := New()
	reg.Base, reg.Interval = ts.URL, 0
	return reg, &hits
}

func TestFindTheOneWithThatName(t *testing.T) {
	reg, hits := site(t, searchPage, companyPage)
	// Lower case and short forms, the way a posting writes it.
	rec, err := reg.Find(context.Background(), "Cty TNHH TM SX Thiên Ấn")
	if err != nil {
		t.Fatal(err)
	}
	want := Record{
		TaxCode: "0309542960", Name: "CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN", Founded: "2009-10-20",
		Status: "NNT đã được cấp MST",
		URL:    "https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-cong-ty-tnhh-thuong-mai-san-xuat-thien-an.html",
	}
	if rec != want {
		t.Fatalf("got %+v\nwant %+v", rec, want)
	}
	// Asked again (any board on this server): no request.
	before := hits.Load()
	if _, err := reg.Find(context.Background(), "CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN"); err != nil || hits.Load() != before {
		t.Fatalf("second lookup: err %v, %d more requests", err, hits.Load()-before)
	}
}

func TestAccentsMakeADifferentCompany(t *testing.T) {
	reg, _ := site(t, searchPage, companyPage)
	// THIÊN AN is a different company, and its page isn't the one read.
	if _, err := reg.Find(context.Background(), "Công ty TNHH Thương mại Sản xuất Thiên"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a name no result has exactly: %v, want ErrNotFound", err)
	}
	if _, err := reg.Find(context.Background(), "FPT Software"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a brand name: %v, want ErrNotFound", err)
	}
}

// The register often holds a name twice (a later, duplicate registration):
// the company is the one still operating that started earliest.
func TestSameNameTwicePicksTheEarliestOperating(t *testing.T) {
	twice := strings.Replace(searchPage, "3701071364-cong-ty-tnhh-san-xuat-thuong-mai-an-thien.html\" class=\"tax-company-link\">\n        CÔNG TY TNHH SẢN XUẤT THƯƠNG MẠI AN THIÊN",
		"0309542961-cong-ty-tnhh-thuong-mai-san-xuat-thien-an.html\" class=\"tax-company-link\">\n        Công Ty TNHH Thương Mại Sản Xuất Thiên Ấn", 1)
	later := strings.NewReplacer(`"taxID":"0309542960","foundingDate":"2009-10-20"`, `"taxID":"0309542961","foundingDate":"2016-01-05"`).Replace(companyPage)
	older := strings.Replace(companyPage, `"foundingDate":"2009-10-20"`, `"foundingDate":"2001-02-03"`, 1)
	closed := strings.Replace(older, "NNT đã được cấp MST", "NNT ngừng hoạt động và đã đóng MST", 1)
	for _, c := range []struct {
		name         string
		first        string
		wantTax, day string
	}{
		{"the earlier one", older, "0309542960", "2001-02-03"},
		{"the later one, the earlier having closed", closed, "0309542961", "2016-01-05"},
	} {
		reg, _ := siteWith(t, twice, map[string]string{"0309542960": c.first, "0309542961": later})
		rec, err := reg.Find(context.Background(), "CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN")
		if err != nil || rec.TaxCode != c.wantTax || rec.Founded != c.day {
			t.Errorf("%s: got %s %s, %v; want %s %s", c.name, rec.TaxCode, rec.Founded, err, c.wantTax, c.day)
		}
	}
}

func TestDateFromTheTableWhenTheBlockHasNone(t *testing.T) {
	reg, _ := site(t, searchPage, strings.Replace(companyPage, `,"foundingDate":"2009-10-20"`, "", 1))
	rec, err := reg.Find(context.Background(), "CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN")
	if err != nil || rec.Founded != "2009-10-20" {
		t.Fatalf("got %q, %v; want the table's 20/10/2009", rec.Founded, err)
	}
}

func TestTroubleIsNotRemembered(t *testing.T) {
	reg, hits := site(t, "<html>Just a moment...</html>", companyPage)
	for range 2 {
		if _, err := reg.Find(context.Background(), "Thiên Ấn"); err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("a page that isn't the search: %v, want an error that isn't ErrNotFound", err)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("%d requests, want 2: trouble must be retried, not remembered", hits.Load())
	}
}

func TestRequestsKeepTheirDistance(t *testing.T) {
	reg, _ := site(t, searchPage, companyPage)
	reg.Interval = 150 * time.Millisecond
	start := time.Now()
	if _, err := reg.Find(context.Background(), "CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("search and company page %v apart, want at least the interval", d)
	}
}

func TestKey(t *testing.T) {
	for in, want := range map[string]string{
		"Công ty CP Du lịch Sen Việt":      "CÔNG TY CỔ PHẦN DU LỊCH SEN VIỆT",
		"CTCP  Sen Việt.":                  "CÔNG TY CỔ PHẦN SEN VIỆT",
		"Công ty TNHH MTV Cà phê Mộc (VN)": "CÔNG TY TNHH MỘT THÀNH VIÊN CÀ PHÊ MỘC VN",
		"Thie\u0302n A\u0302\u0301n":       "THIÊN ẤN", // decomposed accents, as some pages send them
	} {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
	if SearchURL("Cty Thiên Ấn") != Site+"/cong-cu/tra-cuu-ma-so-thue.html?keyword=C%C3%94NG+TY+THI%C3%8AN+%E1%BA%A4N" {
		t.Errorf("SearchURL: %s", SearchURL("Cty Thiên Ấn"))
	}
}
