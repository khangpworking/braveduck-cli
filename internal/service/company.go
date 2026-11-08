package service

import (
	"context"
	"errors"

	"github.com/khangpworking/braveduck-cli/internal/core/company"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Every job's company is looked up in the business register (dulieuphapluat.vn,
// see core/company) once, in the background: when it started operating, its
// registered name and whether it still operates, with a link to its page.
// The lookup is by name, so it happens when a job arrives on the board and
// again only if its company is edited.

// companyFinder is the register; swapped in tests.
type companyFinder interface {
	Find(ctx context.Context, name string) (company.Record, error)
}

// SetCompanyRegister turns the lookups on (`braveduck serve`; nil turns them
// off). Off by default, so nothing reaches the register unless asked.
func (s *Service) SetCompanyRegister(r companyFinder) { s.reg = r }

// MaxCompanyLookupsPerPass keeps each pass short: a lookup is two pages at
// least 2 s apart, and the register's search can take 15 s.
const MaxCompanyLookupsPerPass = 10

// CompanyResult is what one UpdateCompanies pass did.
type CompanyResult struct {
	Filled  int // jobs given what the register says
	Lookups int // company names sent to the register
	Pending int // jobs left for a later pass (lookup cap)
}

// UpdateCompanies fills in the register's answer for every open job not yet
// looked up: names already asked come from the cache, up to maxLookups new
// ones go to the register. Trouble reaching it leaves the job for next time.
func (s *Service) UpdateCompanies(ctx context.Context, maxLookups int) (CompanyResult, error) {
	var res CompanyResult
	if s.reg == nil || !s.coRunning.CompareAndSwap(false, true) {
		return res, nil // off, or a pass is already running; the next one gets the rest
	}
	defer s.coRunning.Store(false)
	all, err := s.st.List(ctx, store.ListFilter{})
	if err != nil {
		return res, err
	}
	for _, j := range all {
		key := company.Key(j.Company)
		if j.CompanyActiveDate != "" || j.Status == "closed" || key == "" {
			continue
		}
		c, cached, err := s.st.GetCompany(ctx, key)
		if err != nil {
			return res, err
		}
		if !cached {
			if res.Lookups >= maxLookups {
				res.Pending++
				continue
			}
			res.Lookups++
			rec, err := s.reg.Find(ctx, j.Company)
			switch {
			case errors.Is(err, company.ErrNotFound):
				c = store.CompanyInfo{URL: company.SearchURL(j.Company)}
			case err != nil:
				if ctx.Err() != nil {
					return res, ctx.Err()
				}
				continue // the register is unreachable or changed: try again next pass
			default:
				c = store.CompanyInfo{Found: true, TaxCode: rec.TaxCode, Name: rec.Name, Founded: rec.Founded, Status: rec.Status, URL: rec.URL}
			}
			if err := s.st.PutCompany(ctx, key, c, s.now()); err != nil {
				return res, err
			}
		}
		date := c.Founded
		switch {
		case !c.Found:
			date = jobs.CompanyNotFound
		case date == "":
			date = jobs.CompanyNoDate
		}
		if err := s.st.SetJobCompany(ctx, j.ID, date, c); err != nil {
			return res, err
		}
		res.Filled++
	}
	return res, nil
}
