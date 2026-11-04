package crawl

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
)

// CareerLink searches careerlink.vn's public job search
// (/vieclam/list?keyword=…, allowed by its robots.txt) and reads each
// result's detail page, whose schema.org JobPosting has the full posting.
// Result cards carry no description and their CSS class names change with
// every deploy, so only the job links are taken from the search page.
// Polite by construction: one search page per query, at most PerQuery detail
// pages, one request at a time with Delay between them; the service's
// schedule decides how often.
type CareerLink struct {
	Base     string // https://www.careerlink.vn; overridable in tests
	Client   *http.Client
	PerQuery int
	Delay    time.Duration

	known func(url string) bool // see SkipKnown
}

// SkipKnown makes Search skip the detail page of listings the board has.
func (c *CareerLink) SkipKnown(known func(url string) bool) { c.known = known }

// NewCareerLink returns the crawler with production settings.
func NewCareerLink() *CareerLink {
	return &CareerLink{
		Base:     "https://www.careerlink.vn",
		Client:   &http.Client{Timeout: 30 * time.Second},
		PerQuery: 10,
		Delay:    time.Second,
	}
}

func (c *CareerLink) Name() string { return "careerlink" }

// careerLinkJobRE matches a result's detail link: /tim-viec-lam/<slug>/<id>;
// careerLinkTitleRE the same link with the title attribute results carry.
var (
	careerLinkJobRE   = regexp.MustCompile(`href="(/tim-viec-lam/[a-z0-9-]+/[0-9]+)"`)
	careerLinkTitleRE = regexp.MustCompile(`<a [^>]*title="([^"]+)"[^>]*href="(/tim-viec-lam/[a-z0-9-]+/[0-9]+)"`)
)

// Search returns up to PerQuery listings for query, in the site's order.
func (c *CareerLink) Search(ctx context.Context, query string) ([]Listing, error) {
	page, err := c.get(ctx, c.Base+"/vieclam/list?keyword="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	var paths []string
	seen := map[string]bool{}
	for _, m := range careerLinkJobRE.FindAllStringSubmatch(page, -1) {
		if !seen[m[1]] && len(paths) < c.PerQuery {
			seen[m[1]] = true
			paths = append(paths, m[1])
		}
	}

	titles := map[string]string{}
	for _, m := range careerLinkTitleRE.FindAllStringSubmatch(page, -1) {
		titles[m[2]] = html.UnescapeString(m[1])
	}

	var listings []Listing
	fetched := 0
	for _, p := range paths {
		u := "https://www.careerlink.vn" + p
		if c.known != nil && c.known(u) {
			listings = append(listings, Listing{Title: titles[p], URL: u}) // on the board: no detail fetch
			continue
		}
		if fetched++; fetched > 1 && c.Delay > 0 {
			select {
			case <-ctx.Done():
				return listings, ctx.Err()
			case <-time.After(c.Delay):
			}
		}
		detail, err := c.get(ctx, c.Base+p)
		if err != nil {
			if ctx.Err() != nil {
				return listings, ctx.Err()
			}
			continue // one unreadable posting doesn't fail the query
		}
		jp, ok := scrape.ExtractJobPosting(detail)
		if !ok || jp.Title == "" {
			continue
		}
		listings = append(listings, Listing{
			Title:       jp.Title,
			Company:     jp.Company,
			URL:         u,
			Location:    jp.Location,
			Salary:      jp.Salary,
			Skills:      jp.Skills,
			Description: jp.Description,
			Deadline:    jp.Deadline,
			Email:       jp.Email,
		})
	}
	return listings, nil
}

// get fetches one page; a non-200 is an error naming the status.
func (c *CareerLink) get(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0 braveduck")
	req.Header.Set("Accept-Language", "vi,en;q=0.8")
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("careerlink: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("careerlink: HTTP %d for %s", resp.StatusCode, strings.TrimPrefix(u, c.Base))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}
