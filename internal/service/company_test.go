package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/company"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

type fakeRegister struct {
	known map[string]company.Record
	down  bool
	asked []string
}

func (f *fakeRegister) Find(_ context.Context, name, _ string) (company.Record, error) {
	f.asked = append(f.asked, name)
	if f.down {
		return company.Record{}, errors.New("business register: HTTP 503")
	}
	if r, ok := f.known[company.Key(name)]; ok {
		return r, nil
	}
	return company.Record{}, company.ErrNotFound
}

const thienAn = "CÔNG TY TNHH THƯƠNG MẠI SẢN XUẤT THIÊN ẤN"

func TestEveryCompanyIsLookedUpOnce(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if res, _ := s.UpdateCompanies(ctx, 10); res != (CompanyResult{}) {
		t.Fatalf("with no register set: %+v, want nothing done", res)
	}
	reg := &fakeRegister{known: map[string]company.Record{
		thienAn:               {TaxCode: "0309542960", Name: thienAn, Founded: "2009-10-20", Status: "NNT đang hoạt động", URL: "https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-x.html"},
		"CÔNG TY CỔ PHẦN MỚI": {TaxCode: "0312345678", Name: "CÔNG TY CỔ PHẦN MỚI", Status: "NNT ngừng hoạt động và đã đóng MST", URL: "https://dulieuphapluat.vn/cong-cu/ma-so-thue/0312345678-y.html"},
	}}
	s.SetCompanyRegister(reg)
	a, _ := s.AddJob(ctx, NewJob{Title: "Kế toán", Company: "Cty TNHH TM SX Thiên Ấn"})
	b, _ := s.AddJob(ctx, NewJob{Title: "Thu ngân", Company: thienAn}) // the same company, written out
	brand, _ := s.AddJob(ctx, NewJob{Title: "Dev", Company: "FPT Software"})
	closed, _ := s.AddJob(ctx, NewJob{Title: "Kho", Company: "Công ty CP Mới"})
	none, _ := s.AddJob(ctx, NewJob{Title: "No company"})
	old, _ := s.AddJob(ctx, NewJob{Title: "Old", Company: "Công ty Đã Đóng"})
	s.UpdateJob(ctx, old.ID, Patch{Status: ptr("closed")})

	first, err := s.UpdateCompanies(ctx, 2)
	if err != nil || first.Lookups != 2 || first.Pending == 0 {
		t.Fatalf("first pass = %+v, %v; want 2 lookups (the cap) and the rest left", first, err)
	}
	second, _ := s.UpdateCompanies(ctx, 2)
	if second.Lookups != 1 || second.Pending != 0 || first.Filled+second.Filled != 4 {
		t.Fatalf("second pass = %+v after %+v; want the one name left, and 4 jobs filled in all (Thiên Ấn's twice)", second, first)
	}
	var res CompanyResult
	if res, _ = s.UpdateCompanies(ctx, 2); res != (CompanyResult{}) {
		t.Fatalf("third pass = %+v; every job already has its answer", res)
	}
	if len(reg.asked) != 3 {
		t.Fatalf("asked %q: each company once, none for a job without one or a closed job", reg.asked)
	}

	for _, j := range []jobs.Job{a, b} {
		got, _ := s.GetJob(ctx, j.ID)
		if got.CompanyActiveDate != "2009-10-20" || got.CompanyTaxCode != "0309542960" || got.CompanyLegalName != thienAn ||
			got.CompanyInfoURL != "https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-x.html" || got.CompanyRegStatus != "NNT đang hoạt động" {
			t.Errorf("%s: %+v", j.Title, got)
		}
	}
	got, _ := s.GetJob(ctx, brand.ID)
	if got.CompanyActiveDate != jobs.CompanyNotFound || got.CompanyInfoURL != company.SearchURL("FPT Software") || got.CompanyTaxCode != "" {
		t.Errorf("not found: date %q, link %q; want the sentinel and the register's search", got.CompanyActiveDate, got.CompanyInfoURL)
	}
	got, _ = s.GetJob(ctx, closed.ID)
	if got.CompanyActiveDate != jobs.CompanyNoDate || !slices.Contains(got.Dealbreakers, "Cty ngừng hoạt động") {
		t.Errorf("a company that closed: date %q, warnings %q", got.CompanyActiveDate, got.Dealbreakers)
	}
	if got, _ := s.GetJob(ctx, none.ID); got.CompanyActiveDate != "" {
		t.Errorf("no company: %q", got.CompanyActiveDate)
	}

	// A new company name is looked up again; the same name keeps its answer.
	s.UpdateJob(ctx, a.ID, Patch{Company: ptr("Cty TNHH TM SX Thiên Ấn")})
	if got, _ := s.GetJob(ctx, a.ID); got.CompanyActiveDate != "2009-10-20" {
		t.Errorf("saving the same name dropped the answer: %q", got.CompanyActiveDate)
	}
	s.UpdateJob(ctx, a.ID, Patch{Company: ptr("Công ty CP Mới")})
	if got, _ := s.GetJob(ctx, a.ID); got.CompanyActiveDate != "" || got.CompanyTaxCode != "" || got.CompanyInfoURL != "" {
		t.Errorf("a new name kept the old company's answer: %+v", got)
	}
	if res, _ := s.UpdateCompanies(ctx, 2); res.Lookups != 0 || res.Filled != 1 {
		t.Errorf("the new name is cached already: %+v", res)
	}
}

func TestRegisterTroubleIsRetried(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	reg := &fakeRegister{down: true}
	s.SetCompanyRegister(reg)
	j, _ := s.AddJob(ctx, NewJob{Title: "Kế toán", Company: thienAn})
	if res, err := s.UpdateCompanies(ctx, 10); err != nil || res.Filled != 0 {
		t.Fatalf("register down: %+v, %v", res, err)
	}
	if got, _ := s.GetJob(ctx, j.ID); got.CompanyActiveDate != "" {
		t.Fatalf("trouble was saved as an answer: %q", got.CompanyActiveDate)
	}
	reg.down, reg.known = false, map[string]company.Record{thienAn: {TaxCode: "0309542960", Name: thienAn, Founded: "2009-10-20"}}
	if res, _ := s.UpdateCompanies(ctx, 10); res.Lookups != 1 || res.Filled != 1 {
		t.Fatalf("back up: %+v; want it asked again", res)
	}
}

// A post from a Facebook group may give the group's name as its company;
// that isn't a company to look up.
func TestAGroupsNameIsNotACompany(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	reg := &fakeRegister{}
	s.SetCompanyRegister(reg)
	s.FollowGroup(ctx, "https://www.facebook.com/groups/1329241984892387/")
	s.SetGroupStatus(ctx, "1329241984892387", "Việc làm AI Engineer -Việt Nam", FBPublic, "")
	s.AddJob(ctx, NewJob{Title: "AI Engineer", Company: "Việc làm AI Engineer -Việt Nam", Source: "facebook"})
	if res, _ := s.UpdateCompanies(ctx, 10); res.Lookups != 0 || len(reg.asked) != 0 {
		t.Fatalf("looked up a group's name: %+v %q", res, reg.asked)
	}
}

type registerFunc func(context.Context, string, string) (company.Record, error)

func (f registerFunc) Find(ctx context.Context, name, location string) (company.Record, error) {
	return f(ctx, name, location)
}

func TestLookupDoesNotOverwriteAnEditOrAnAnswer(t *testing.T) {
	for _, answered := range []bool{false, true} {
		t.Run(fmt.Sprint(answered), func(t *testing.T) {
			s := newSvc(t)
			ctx := context.Background()
			j, err := s.AddJob(ctx, NewJob{Title: "Kế toán", Company: thienAn})
			if err != nil {
				t.Fatal(err)
			}
			s.SetCompanyRegister(registerFunc(func(ctx context.Context, name, location string) (company.Record, error) {
				if answered {
					changed, err := s.st.SetJobCompany(ctx, j.ID, name, "2001-01-01", store.CompanyInfo{TaxCode: "0101778163"})
					if err != nil || !changed {
						t.Fatalf("concurrent answer: %v, %v", changed, err)
					}
				} else {
					if _, err := s.UpdateJob(ctx, j.ID, Patch{Company: ptr("New employer")}); err != nil {
						t.Fatal(err)
					}
				}
				return company.Record{TaxCode: "0309542960", Founded: "2009-10-20", Name: thienAn}, nil
			}))
			res, err := s.UpdateCompanies(ctx, 10)
			if err != nil || res.Filled != 0 || res.Lookups != 1 {
				t.Fatalf("stale lookup: %+v, %v", res, err)
			}
			got, err := s.GetJob(ctx, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if answered {
				if got.CompanyActiveDate != "2001-01-01" || got.CompanyTaxCode != "0101778163" {
					t.Fatalf("overwrote newer answer: %+v", got)
				}
			} else if got.Company != "New employer" || got.CompanyActiveDate != "" || got.CompanyTaxCode != "" {
				t.Fatalf("attached old identity to edited company: %+v", got)
			}
		})
	}
}

func TestCachedAnswerAlsoGuardsTheSnapshotName(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	target, err := s.st.Add(ctx, jobs.Job{Title: "Older job", Company: thienAn, SavedAt: "2026-10-01T00:00:00Z"}, s.now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.Add(ctx, jobs.Job{Title: "Newer job", Company: "Trigger", SavedAt: "2026-11-01T00:00:00Z"}, s.now()); err != nil {
		t.Fatal(err)
	}
	if err := s.st.PutCompany(ctx, company.LookupKey(thienAn, ""), store.CompanyInfo{Found: true, Founded: "2009-10-20", TaxCode: "0309542960"}, s.now()); err != nil {
		t.Fatal(err)
	}
	s.SetCompanyRegister(registerFunc(func(ctx context.Context, name, location string) (company.Record, error) {
		if name != "Trigger" {
			t.Fatalf("cached name was queried: %s", name)
		}
		if _, err := s.UpdateJob(ctx, target, Patch{Company: ptr("Edited employer")}); err != nil {
			t.Fatal(err)
		}
		return company.Record{}, company.ErrNotFound
	}))
	res, err := s.UpdateCompanies(ctx, 10)
	if err != nil || res.Filled != 1 || res.Lookups != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
	got, err := s.GetJob(ctx, target)
	if err != nil || got.Company != "Edited employer" || got.CompanyActiveDate != "" {
		t.Fatalf("stale cached answer: %+v, %v", got, err)
	}
}

func TestPersistentCompanyCacheUsesWorkplaceProvince(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	var asked []string
	s.SetCompanyRegister(registerFunc(func(ctx context.Context, name, location string) (company.Record, error) {
		asked = append(asked, location)
		switch location {
		case "Hà Nội":
			return company.Record{TaxCode: "0101778163", Founded: "2005-01-01"}, nil
		case "TP HCM":
			return company.Record{TaxCode: "0310409233", Founded: "2010-10-26"}, nil
		default:
			return company.Record{}, company.ErrNotFound
		}
	}))
	for i, tc := range []struct{ location, want string }{
		{"Remote", ""}, {"Hà Nội", "0101778163"}, {"TP HCM", "0310409233"}, {"Bình Dương", "0310409233"},
	} {
		id, err := s.st.Add(ctx, jobs.Job{Title: fmt.Sprint(i), Company: thienAn, Location: tc.location}, s.now())
		if err != nil {
			t.Fatal(err)
		}
		if res, err := s.UpdateCompanies(ctx, 10); err != nil || res.Filled != 1 {
			t.Fatalf("%+v, %v", res, err)
		}
		got, err := s.GetJob(ctx, id)
		if err != nil || got.CompanyTaxCode != tc.want {
			t.Fatalf("%s: %+v, %v", tc.location, got, err)
		}
	}
	if !slices.Equal(asked, []string{"Remote", "Hà Nội", "TP HCM"}) {
		t.Fatalf("looked up %q", asked)
	}
}
