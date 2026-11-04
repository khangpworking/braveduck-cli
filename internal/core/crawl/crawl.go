// Package crawl holds braveduck's built-in job crawlers. A crawler turns a
// search query into job listings; the service decides what to keep (keyword
// filters, duplicates) and when to run (the per-source schedule).
package crawl

import (
	"context"
	"html"
	"regexp"
	"strings"
)

// Listing is one job a crawler found, normalized across sources.
type Listing struct {
	Title       string
	Company     string
	URL         string // canonical listing URL — braveduck's duplicate key
	Location    string
	Salary      string
	Skills      []string
	Description string // plain text
	Deadline    string // YYYY-MM-DD, "" if unknown
	Email       string
	// Lat/Lng are the workplace coordinates when the source provides them
	// (used for distance once the user sets a location). HasGeo reports it.
	Lat, Lng float64
	HasGeo   bool
}

// Crawler fetches listings for one query from one source.
type Crawler interface {
	// Name is the source id: the job's discovery_source and the schedule key.
	Name() string
	// Search returns the newest listings for query, newest first.
	Search(ctx context.Context, query string) ([]Listing, error)
}

// Builtin returns braveduck's own crawlers.
func Builtin() []Crawler {
	return []Crawler{NewVietnamWorks()}
}

var (
	tagRE   = regexp.MustCompile(`(?s)<[^>]+>`)
	blockRE = regexp.MustCompile(`(?i)<\s*(br|/p|/li|/div|/h[1-6])\s*/?>`)
	wsRE    = regexp.MustCompile(`[ \t\x{00a0}]+`)
	nlRE    = regexp.MustCompile(`\n{3,}`)
)

// htmlToText turns a listing's HTML description into readable plain text,
// keeping line breaks at block boundaries.
func htmlToText(s string) string {
	s = blockRE.ReplaceAllString(s, "\n")
	s = strings.ReplaceAll(s, "<li>", "\n• ")
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimSpace(wsRE.ReplaceAllString(l, " ")))
	}
	return strings.TrimSpace(nlRE.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
