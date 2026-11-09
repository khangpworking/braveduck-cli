package company

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const legalName = "CÔNG TY CỔ PHẦN CÔNG NGHỆ PREP"
const englishBosch = "BOSCH GLOBAL SOFTWARE TECHNOLOGIES COMPANY LIMITED"
const vietnameseBosch = "CÔNG TY TNHH BOSCH GLOBAL SOFTWARE TECHNOLOGIES"

func detailPath(tax string) string { return "/cong-cu/ma-so-thue/" + tax + "-company.html" }

func detail(tax, name, alternate, address string) string {
	b, _ := json.Marshal(map[string]any{
		"@type": "Organization", "taxID": tax, "legalName": name,
		"alternateName": alternate, "foundingDate": "2021-11-15",
		"address": map[string]string{"streetAddress": address},
	})
	return `<script type="application/ld+json">` + string(b) + `</script>`
}

func searchResult(tax, name string) string {
	return fmt.Sprintf(`<a href="%s" class="tax-company-link">%s</a>`, detailPath(tax), name)
}

func TestSameNameUsesProvinceAndScopesCache(t *testing.T) {
	search := `<div class="tax-result-list">` + searchResult("0109817671", legalName) + searchResult("0310409233", legalName) + `</div>`
	for _, tc := range []struct{ name, first, second, location, want string }{
		{"Hanoi", "Ha Noi", "Hồ Chí Minh", "Hà Nội", "0109817671"},
		{"merged province", "Ha Noi", "Hồ Chí Minh", "Bình Dương", "0310409233"},
		{"still ambiguous", "Hà Nội", "Ha Noi", "Hà Nội", ""},
		{"neither", "Hà Nội", "Ho Chi Minh", "Đà Nẵng", ""},
		{"unknown job", "Hà Nội", "Ho Chi Minh", "Remote", ""},
		{"unknown register", "", "", "Ha Noi", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, _ := siteWith(t, search, map[string]string{
				"0109817671": detail("0109817671", legalName, "", tc.first),
				"0310409233": detail("0310409233", legalName, "", tc.second),
			})
			rec, err := reg.Find(context.Background(), legalName, tc.location)
			if tc.want == "" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("got %+v, %v; want not found", rec, err)
				}
			} else if err != nil || rec.TaxCode != tc.want {
				t.Fatalf("got %+v, %v; want %s", rec, err, tc.want)
			}
		})
	}
	reg, requests := siteWith(t, search, map[string]string{
		"0109817671": detail("0109817671", legalName, "", "Hà Nội"),
		"0310409233": detail("0310409233", legalName, "", "TP HCM"),
	})
	for _, tc := range []struct{ location, tax string }{
		{"Đà Nẵng", ""}, {"Ha Noi", "0109817671"}, {"Bình Dương", "0310409233"}, {"TP HCM", "0310409233"},
	} {
		rec, err := reg.Find(context.Background(), legalName, tc.location)
		if rec.TaxCode != tc.tax || (tc.tax != "" && err != nil) {
			t.Fatalf("%s: %+v, %v", tc.location, rec, err)
		}
	}
	if got := requests.Load(); got != 9 {
		t.Fatalf("%d requests, want 9 (merged province reuses answer)", got)
	}
}

func TestSingleLegalNameAllowsBranchOffice(t *testing.T) {
	reg, _ := siteWith(t, `<div class="tax-result-list">`+searchResult("0109817671", legalName), map[string]string{
		"0109817671": detail("0109817671", legalName, "", "Hà Nội"),
	})
	for _, location := range []string{"Ho Chi Minh", "Remote"} {
		if rec, err := reg.Find(context.Background(), legalName, location); err != nil || rec.TaxCode != "0109817671" {
			t.Fatalf("%s: %+v, %v", location, rec, err)
		}
	}
}

func TestRedirectUsesFetchedBodyAndPacesEveryHop(t *testing.T) {
	for _, hops := range []int{1, 2} {
		t.Run(fmt.Sprint(hops), func(t *testing.T) {
			var requests atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n <= int32(hops) {
					target := detailPath("0109817671")
					if n < int32(hops) {
						target = searchPath + "?keyword=PREP"
					}
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				fmt.Fprint(w, detail("0109817671", legalName, "", "Ha Noi"))
			}))
			defer ts.Close()
			reg := New()
			reg.Base, reg.Interval = ts.URL, 50*time.Millisecond
			start := time.Now()
			rec, err := reg.Find(context.Background(), legalName, "Ha Noi")
			if err != nil || rec.TaxCode != "0109817671" {
				t.Fatalf("%+v, %v", rec, err)
			}
			if requests.Load() != int32(hops+1) || time.Since(start) < time.Duration(hops)*reg.Interval {
				t.Fatalf("redirect not paced or fetched twice: requests %d, elapsed %v", requests.Load(), time.Since(start))
			}
		})
	}
}

func TestUnsafeAndExcessRedirectsAreRetryable(t *testing.T) {
	var external atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { external.Add(1) }))
	defer other.Close()
	for _, target := range []string{other.URL + detailPath("0109817671"), "/unrelated", searchPath + "?again=1"} {
		t.Run(target, func(t *testing.T) {
			var requests atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer ts.Close()
			reg := New()
			reg.Base, reg.Interval = ts.URL, 0
			for range 2 {
				if _, err := reg.Find(context.Background(), legalName, "Ha Noi"); err == nil || errors.Is(err, ErrNotFound) {
					t.Fatalf("unsafe redirect: %v", err)
				}
			}
			want := int32(2)
			if strings.HasPrefix(target, searchPath) {
				want = 6
			}
			if requests.Load() != want {
				t.Fatalf("%d requests, want %d", requests.Load(), want)
			}
		})
	}
	if external.Load() != 0 {
		t.Fatal("followed redirect to another host")
	}
}

func TestPageIdentityAndDateErrorsAreNotCached(t *testing.T) {
	for _, tc := range []struct{ name, page string }{
		{"wrong name", detail("0109817671", "CÔNG TY KHÁC", "", "Ha Noi")},
		{"wrong tax code", detail("0310409233", legalName, "", "Ha Noi")},
		{"branch code", detail("0109817671-001", legalName, "", "Ha Noi")},
		{"invalid date", strings.Replace(detail("0109817671", legalName, "", "Ha Noi"), "2021-11-15", "2021-02-30", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, requests := siteWith(t, `<div class="tax-result-list">`+searchResult("0109817671", legalName), map[string]string{"0109817671": tc.page})
			for range 2 {
				if _, err := reg.Find(context.Background(), legalName, "Ha Noi"); err == nil || errors.Is(err, ErrNotFound) {
					t.Fatalf("got %v, want retryable error", err)
				}
			}
			if requests.Load() != 4 {
				t.Fatalf("error cached: %d requests", requests.Load())
			}
		})
	}
	reg, _ := site(t, searchPage, strings.NewReplacer(`,"foundingDate":"2009-10-20"`, "", "20/10/2009", "31/02/2009").Replace(companyPage))
	if _, err := reg.Find(context.Background(), "Cty TNHH TM SX Thiên Ấn", ""); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("bad table date: %v", err)
	}
}

func TestEnglishDiscoveryRequiresIdentityAndProvince(t *testing.T) {
	for _, tc := range []struct {
		name, alternate, registered, location string
		wantError                             bool
		wantTax                               string
	}{
		{"Bosch", englishBosch, "Hồ Chí Minh", "Bình Dương", false, "0310409233"},
		{"wrong province", englishBosch, "Ha Noi", "HCM", false, ""},
		{"unknown register", englishBosch, "", "HCM", false, ""},
		{"unknown workplace", englishBosch, "HCM", "Remote", false, ""},
		// Another company's name is an answer (no match), not trouble to retry.
		{"different English name", "BOSCH SERVICE COMPANY LIMITED", "HCM", "HCM", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == searchPath {
					fmt.Fprint(w, `<div class="tax-result-list">`)
					if r.URL.Query().Get("keyword") == "BOSCH GLOBAL SOFTWARE TECHNOLOGIES" {
						fmt.Fprint(w, searchResult("0310409233", vietnameseBosch))
						fmt.Fprint(w, searchResult("0109817671", "CÔNG TY TNHH BOSCH"))
					}
					return
				}
				if r.URL.Path != detailPath("0310409233") {
					t.Error("opened candidate without distinctive words")
				}
				fmt.Fprint(w, detail("0310409233", vietnameseBosch, tc.alternate, tc.registered))
			}))
			defer ts.Close()
			reg := New()
			reg.Base, reg.Interval = ts.URL, 0
			rec, err := reg.Find(context.Background(), englishBosch, tc.location)
			if rec.TaxCode != tc.wantTax || (tc.wantTax != "" && err != nil) {
				t.Fatalf("%+v, %v", rec, err)
			}
			if tc.wantError && (err == nil || errors.Is(err, ErrNotFound)) {
				t.Fatalf("identity mismatch: %v", err)
			}
			if tc.wantTax == "" && !tc.wantError && !errors.Is(err, ErrNotFound) {
				t.Fatalf("province mismatch: %v", err)
			}
			if requests.Load() > 3 {
				t.Fatalf("%d requests", requests.Load())
			}
		})
	}
}

func TestLookupBudgetCannotChooseFromPartialExactMatches(t *testing.T) {
	search := `<div class="tax-result-list">`
	pages := map[string]string{}
	for i := range 5 {
		tax := fmt.Sprintf("010981767%d", i)
		search += searchResult(tax, legalName)
		pages[tax] = detail(tax, legalName, "", "Ha Noi")
	}
	reg, requests := siteWith(t, search, pages)
	if _, err := reg.Find(context.Background(), legalName, "Ha Noi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v, want unresolved ambiguity", err)
	}
	if requests.Load() > 5 {
		t.Fatalf("%d requests, budget is 5", requests.Load())
	}
}

func TestEnglishCandidateAndTotalRequestLimits(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			var requests, details atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == searchPath {
					if redirect && r.URL.Query().Get("hop") == "" {
						http.Redirect(w, r, r.URL.String()+"&hop=1", http.StatusFound)
						return
					}
					fmt.Fprint(w, `<div class="tax-result-list">`)
					if r.URL.Query().Get("keyword") != englishBosch {
						for i := range 4 {
							fmt.Fprint(w, searchResult(fmt.Sprintf("031040923%d", i), vietnameseBosch))
						}
					}
					return
				}
				details.Add(1)
				tax := strings.Split(strings.TrimPrefix(r.URL.Path, "/cong-cu/ma-so-thue/"), "-")[0]
				fmt.Fprint(w, detail(tax, vietnameseBosch, englishBosch, "Ha Noi"))
			}))
			defer ts.Close()
			reg := New()
			reg.Base, reg.Interval = ts.URL, 0
			_, err := reg.Find(context.Background(), englishBosch, "HCM")
			if err == nil {
				t.Fatal("accepted wrong province")
			}
			if requests.Load() != 5 || details.Load() > 3 {
				t.Fatalf("requests=%d, details=%d", requests.Load(), details.Load())
			}
		})
	}
}

// One company listed twice (same name, same registered address, two tax
// codes, as FPT Telecom is) is one company: its earlier date.
func TestOneCompanyListedTwiceIsOne(t *testing.T) {
	const addr = "Tầng 2, tòa nhà FPT Cầu Giấy, số 17 phố Duy Tân, Phường Cầu Giấy, TP Hà Nội, Việt Nam."
	search := `<div class="tax-result-list">` + searchResult("0105977674", legalName) + searchResult("0101778163", legalName) + `</div>`
	later := strings.Replace(detail("0105977674", legalName, "", addr), "2021-11-15", "2012-08-22", 1)
	earlier := strings.Replace(detail("0101778163", legalName, "", addr), "2021-11-15", "2005-09-21", 1)
	reg, _ := siteWith(t, search, map[string]string{"0105977674": later, "0101778163": earlier})
	rec, err := reg.Find(context.Background(), legalName, "Duy Tân, Cầu Giấy, Hà Nội")
	if err != nil || rec.TaxCode != "0101778163" || rec.Founded != "2005-09-21" {
		t.Fatalf("got %+v, %v; want the 2005 record", rec, err)
	}
	// The same ward written another way is still the one company, and the
	// workplace can be a branch in another province.
	other := strings.Replace(later, "Tầng 2, tòa nhà FPT Cầu Giấy, số 17 phố Duy Tân", "Toà FPT, 17 Duy Tân", 1)
	reg, _ = siteWith(t, search, map[string]string{"0105977674": other, "0101778163": earlier})
	if rec, err := reg.Find(context.Background(), legalName, "Hải Châu, Đà Nẵng"); err != nil || rec.TaxCode != "0101778163" {
		t.Fatalf("a branch in Đà Nẵng: %+v, %v; want the 2005 record", rec, err)
	}
	// Another ward in the same province: two companies, no answer.
	apart := strings.Replace(later, "Phường Cầu Giấy", "Phường Hoàng Mai", 1)
	reg, _ = siteWith(t, search, map[string]string{"0105977674": apart, "0101778163": earlier})
	if _, err := reg.Find(context.Background(), legalName, "Hà Nội"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("two wards: %v, want not found", err)
	}
}

// Two different companies with one name in one city: the workplace's ward
// says which (Lửa Á Châu: one in old Ward 25, one in Đức Nhuận).
func TestTheWorkplaceWardPicksAmongSameNames(t *testing.T) {
	search := `<div class="tax-result-list">` + searchResult("0310174951", legalName) + searchResult("0315089499", legalName) + `</div>`
	old := strings.Replace(detail("0310174951", legalName, "", "Phòng 102, số 441/15B Điện Biên Phủ, Phường 25(Hết hiệu lực), TP Hồ Chí Minh, Việt Nam."), "2021-11-15", "2010-08-01", 1)
	now := strings.Replace(detail("0315089499", legalName, "", "72/24 Phan Đăng Lưu, Phường Đức Nhuận, TP Hồ Chí Minh, Việt Nam."), "2021-11-15", "2018-06-04", 1)
	pages := map[string]string{"0310174951": old, "0315089499": now}
	for _, tc := range []struct{ location, want string }{
		{"Đức Nhuận, Hồ Chí Minh", "0315089499"},
		{"Phường 25, Bình Thạnh, Hồ Chí Minh", "0310174951"},
		{"25 Lê Lợi, Quận 1, Hồ Chí Minh", ""}, // a house number isn't a ward
		{"Hồ Chí Minh", ""},
	} {
		reg, _ := siteWith(t, search, pages)
		rec, err := reg.Find(context.Background(), legalName, tc.location)
		if tc.want == "" && !errors.Is(err, ErrNotFound) || tc.want != "" && rec.TaxCode != tc.want {
			t.Errorf("%s: %s, %v; want %q", tc.location, rec.TaxCode, err, tc.want)
		}
	}
}
