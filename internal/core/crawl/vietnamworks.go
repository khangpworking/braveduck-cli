package crawl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// VietnamWorks searches the public job-search API behind vietnamworks.com
// (vietnamworks-api-analysis docs/search-api.md): one anonymous POST, no
// cookie, key or CSRF token. nginx refuses default library user agents, so a
// browser-like one is sent. Polite by construction: one page per query, and
// the service's schedule decides how often.
type VietnamWorks struct {
	Endpoint string // overridable in tests
	Client   *http.Client
	PerQuery int // hits fetched per query
}

// NewVietnamWorks returns the crawler with production settings.
func NewVietnamWorks() *VietnamWorks {
	return &VietnamWorks{
		Endpoint: "https://ms.vietnamworks.com/job-search/v1.0/search",
		Client:   &http.Client{Timeout: 30 * time.Second},
		PerQuery: 30,
	}
}

func (v *VietnamWorks) Name() string { return "vietnamworks" }

type vnwRange struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"` // must be a string: a number is a 400
}

type vnwOrder struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

type vnwRequest struct {
	UserID      int        `json:"userId"`
	Query       string     `json:"query"`
	Filter      []any      `json:"filter"`
	Ranges      []vnwRange `json:"ranges"`
	Order       []vnwOrder `json:"order"`
	HitsPerPage int        `json:"hitsPerPage"`
	Page        int        `json:"page"` // zero-based
}

type vnwHit struct {
	JobID          json.Number `json:"jobId"`
	JobTitle       string      `json:"jobTitle"`
	CompanyName    string      `json:"companyName"`
	JobURL         string      `json:"jobUrl"`
	PrettySalary   string      `json:"prettySalary"`
	ExpiredOn      string      `json:"expiredOn"`
	JobDescription string      `json:"jobDescription"`
	JobRequirement string      `json:"jobRequirement"`
	EmailAddress   string      `json:"emailAddress"`
	Skills         []struct {
		SkillName string `json:"skillName"`
	} `json:"skills"`
	WorkingLocations []struct {
		Address  string `json:"address"`
		CityName string `json:"cityName"`
		GeoLoc   *struct {
			Lat json.Number `json:"lat"`
			Lon json.Number `json:"lon"`
		} `json:"geoLoc"`
	} `json:"workingLocations"`
}

type vnwResponse struct {
	Data []vnwHit `json:"data"`
}

// Search returns the newest listings matching query.
func (v *VietnamWorks) Search(ctx context.Context, query string) ([]Listing, error) {
	body, _ := json.Marshal(vnwRequest{
		Query:       query,
		Filter:      []any{},
		Ranges:      []vnwRange{},
		Order:       []vnwOrder{{Field: "approvedOn", Value: "desc"}},
		HitsPerPage: v.PerQuery,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "vi")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0 braveduck")
	resp, err := v.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vietnamworks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("vietnamworks: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out vnwResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("vietnamworks: unexpected response (the API may have changed): %w", err)
	}

	var listings []Listing
	for _, h := range out.Data {
		if h.JobTitle == "" || h.JobURL == "" {
			continue
		}
		l := Listing{
			Title:   strings.TrimSpace(h.JobTitle),
			Company: strings.TrimSpace(h.CompanyName),
			URL:     h.JobURL,
			Salary:  strings.TrimSpace(h.PrettySalary),
			Email:   strings.TrimSpace(h.EmailAddress),
		}
		desc := htmlToText(h.JobDescription)
		if req := htmlToText(h.JobRequirement); req != "" {
			desc += "\n\nYêu cầu / Requirements:\n" + req
		}
		l.Description = strings.TrimSpace(desc)
		for _, s := range h.Skills {
			if s.SkillName != "" {
				l.Skills = append(l.Skills, s.SkillName)
			}
		}
		if len(h.WorkingLocations) > 0 {
			w := h.WorkingLocations[0]
			l.Location = strings.TrimSpace(strings.Trim(w.Address+", "+w.CityName, ", "))
			if w.GeoLoc != nil {
				lat, e1 := strconv.ParseFloat(w.GeoLoc.Lat.String(), 64)
				lng, e2 := strconv.ParseFloat(w.GeoLoc.Lon.String(), 64)
				if e1 == nil && e2 == nil && (lat != 0 || lng != 0) {
					l.Lat, l.Lng, l.HasGeo = lat, lng, true
				}
			}
		}
		if t, err := time.Parse(time.RFC3339, h.ExpiredOn); err == nil {
			l.Deadline = t.Format("2006-01-02")
		}
		listings = append(listings, l)
	}
	return listings, nil
}
