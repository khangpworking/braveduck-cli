package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/topcv"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// Status sync: read the user's own application history on a job site, with
// their saved login, and reflect it on the board. Read-only on the site.

// SyncResult is what one sync did.
type SyncResult struct {
	Platform string `json:"platform"`
	Found    int    `json:"found"`   // applications on the site
	Updated  int    `json:"updated"` // board jobs whose site status changed
	Moved    int    `json:"moved"`   // board jobs moved to Applied
	Added    int    `json:"added"`   // applications that weren't on the board yet
}

// fetchApplications reads the history page with the saved session; swapped
// in tests.
var fetchApplications = func(ctx context.Context, s *Service) ([]topcv.Application, error) {
	reg, _, err := s.setup()
	if err != nil {
		return nil, err
	}
	p, err := reg.Get("topcv")
	if err != nil {
		return nil, err
	}
	sess, seed, err := s.PlatformSession(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	b, cleanup, err := browser.Open(ctx, p, sess, seed)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	pg, err := browser.NewPage(b, sess)
	if err != nil {
		return nil, err
	}
	pg = pg.Context(ctx)
	if err := pg.Navigate(topcv.HistoryURL); err != nil {
		return nil, err
	}
	_ = pg.Timeout(30 * time.Second).WaitLoad()
	info, err := pg.Info()
	if err != nil {
		return nil, err
	}
	if strings.Contains(info.URL, "/login") {
		_ = s.st.SetSessionStatus(ctx, p.ID, store.SessionExpired, "", s.now())
		return nil, notConnectedExpired(p.Name, p.ID)
	}
	res, err := pg.Timeout(15 * time.Second).Eval(topcv.ExtractJS)
	if err != nil {
		return nil, fmt.Errorf("reading the history page: %w", err)
	}
	return topcv.Parse(res.Value.Str())
}

func notConnectedExpired(name, id string) error {
	return fmt.Errorf("%w: your %s login has expired — run: braveduck login %s", ErrNotFound, name, id)
}

// SyncTopCV reads the user's TopCV application history and updates the board:
// each application's TopCV status is recorded, jobs still in Saved/Waiting
// move to Applied, and applications not on the board yet are added (Applied).
func (s *Service) SyncTopCV(ctx context.Context) (SyncResult, error) {
	res := SyncResult{Platform: "topcv"}
	apps, err := fetchApplications(ctx, s)
	if err != nil {
		_ = s.ReportCrawl(ctx, "topcv-sync", false, err.Error())
		return res, err
	}
	res.Found = len(apps)

	all, err := s.st.List(ctx, store.ListFilter{})
	if err != nil {
		return res, err
	}
	byTopcvID := map[string]jobs.Job{}
	for _, j := range all {
		if id := topcv.JobIDFromURL(j.URL); id != "" {
			byTopcvID[id] = j
		}
	}

	for _, a := range apps {
		status := a.Status()
		detail := strings.TrimSpace(strings.TrimSpace(a.StatusText + " " + bracket(a.StatusTime)))
		j, onBoard := byTopcvID[a.JobID]
		if !onBoard {
			nj := jobs.Job{
				Title: a.Title, Company: a.Company, URL: a.URL, DiscoverySource: "topcv",
				TopcvStatus: status, ApplyMethod: "platform", ApplyState: "sent", ApplyDetail: detail,
			}
			if d := a.AppliedDate(); d != "" {
				nj.ExpiresAt = jobs.AppliedPrefix + d // the apply-silence clock
			}
			saved, err := s.addQuiet(ctx, nj)
			if err != nil {
				continue // e.g. the same URL was added meanwhile
			}
			if err := s.st.Patch(ctx, saved.ID, store.JobPatch{Status: strPtr("applied")}, s.now()); err == nil {
				res.Added++
			}
			continue
		}
		if j.TopcvStatus != status || j.ApplyDetail != detail {
			if err := s.st.SetTopcvStatus(ctx, j.ID, status, detail, s.now()); err == nil {
				res.Updated++
			}
		}
		if j.Status == "saved" || j.Status == "waiting" {
			if err := s.st.SetStatus(ctx, j.ID, "applied", s.now()); err == nil {
				res.Moved++
			}
		}
	}

	_ = s.ReportCrawl(ctx, "topcv-sync", true, "")
	if res.Added+res.Updated+res.Moved > 0 {
		s.logEvent(ctx, "moved", fmt.Sprintf("TopCV sync: %d updated, %d moved to Applied, %d added", res.Updated, res.Moved, res.Added))
	}
	return res, nil
}

func bracket(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

func strPtr(s string) *string { return &s }
