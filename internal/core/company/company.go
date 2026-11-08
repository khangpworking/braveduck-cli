// Package company looks a company up in Vietnam's business register, by its
// name as a job posting gives it, through dulieuphapluat.vn's public tax-code
// pages: when it started operating, its tax code, its registered name and
// whether it's still operating.
//
// The site allows these pages in robots.txt with a 2-second crawl delay, so
// the client keeps every request 2 s apart (across all users of one server),
// identifies itself, and remembers answers; callers store them too, so each
// company is looked up once. A name only counts as found when the register
// has exactly one company with that exact name; anything less is "not
// found", with a link to the site's search for a person to look themselves.
package company

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
)

// Record is what the register says about one company.
type Record struct {
	TaxCode string // "0309542960", or "0309542960-001" for a branch
	Name    string // the registered name
	Founded string // the day it started operating, YYYY-MM-DD; "" if the page doesn't say
	Status  string // the register's own words, e.g. "NNT đang hoạt động"
	URL     string // the company's page on the site
}

// ErrNotFound means the register has no single company with that exact name.
var ErrNotFound = errors.New("company not found in the business register")

// Site is the default register.
const Site = "https://dulieuphapluat.vn"

// Register is a polite client for the site.
type Register struct {
	Base      string // overridable in tests
	UserAgent string
	Client    *http.Client
	Interval  time.Duration // minimum gap between requests

	mu   sync.Mutex
	last time.Time

	seenMu sync.Mutex
	seen   map[string]result // answers by Key, shared by every board on this server
}

type result struct {
	rec Record
	err error // nil or ErrNotFound; network trouble isn't remembered
}

// New returns a client for dulieuphapluat.vn. Share one per server: the
// gap between requests and the answers it remembers are per client.
func New() *Register {
	return &Register{
		Base:      Site,
		UserAgent: "braveduck-cli (job board; github.com/khangpworking/braveduck-cli)",
		Client:    &http.Client{Timeout: time.Minute}, // its search can take 15 s
		Interval:  2 * time.Second,
	}
}

// SearchURL is the site's own search for name, for a person to look.
func SearchURL(name string) string {
	return Site + "/cong-cu/tra-cuu-ma-so-thue.html?keyword=" + url.QueryEscape(Key(name))
}

// Find looks name up: a search, then the one company page whose name matches.
func (r *Register) Find(ctx context.Context, name string) (Record, error) {
	key := Key(name)
	if key == "" {
		return Record{}, ErrNotFound
	}
	r.seenMu.Lock()
	res, ok := r.seen[key]
	r.seenMu.Unlock()
	if ok {
		return res.rec, res.err
	}
	rec, err := r.find(ctx, name, key)
	if err == nil || errors.Is(err, ErrNotFound) {
		r.seenMu.Lock()
		if r.seen == nil || len(r.seen) >= 5000 {
			r.seen = map[string]result{}
		}
		r.seen[key] = result{rec, err}
		r.seenMu.Unlock()
	}
	return rec, err
}

func (r *Register) find(ctx context.Context, name, key string) (Record, error) {
	page, err := r.get(ctx, r.Base+"/cong-cu/tra-cuu-ma-so-thue.html?keyword="+url.QueryEscape(Key(name)))
	if err != nil {
		return Record{}, err
	}
	if !strings.Contains(page, "tax-result-list") && !strings.Contains(page, "tax-search-result-summary") {
		return Record{}, errors.New("company search: the page has changed")
	}
	var hits []string
	for _, m := range resultRE.FindAllStringSubmatch(page, -1) {
		if Key(html.UnescapeString(m[2])) == key && !slices.Contains(hits, m[1]) {
			hits = append(hits, m[1])
		}
	}
	if len(hits) != 1 {
		return Record{}, ErrNotFound // nothing with that exact name, or several
	}
	return r.company(ctx, r.Base+hits[0])
}

// company reads one company page: its schema.org Organization block, with
// the table rows as a fallback for the date.
func (r *Register) company(ctx context.Context, link string) (Record, error) {
	page, err := r.get(ctx, link)
	if err != nil {
		return Record{}, err
	}
	var rec Record
	for _, m := range jsonLDRE.FindAllStringSubmatch(page, -1) {
		var o struct {
			Type         string `json:"@type"`
			Name         string `json:"name"`
			LegalName    string `json:"legalName"`
			TaxID        string `json:"taxID"`
			FoundingDate string `json:"foundingDate"`
			URL          string `json:"url"`
		}
		if json.Unmarshal([]byte(m[1]), &o) == nil && o.Type == "Organization" && o.TaxID != "" {
			rec = Record{TaxCode: o.TaxID, Name: cmp.Or(o.LegalName, o.Name), Founded: o.FoundingDate, URL: o.URL}
			break
		}
	}
	if rec.TaxCode == "" {
		return Record{}, errors.New("company page: no company data on it (the page has changed)")
	}
	if !dayRE.MatchString(rec.Founded) {
		rec.Founded = ""
		if m := startedRE.FindStringSubmatch(page); m != nil {
			rec.Founded = m[3] + "-" + m[2] + "-" + m[1]
		}
	}
	if m := statusRE.FindStringSubmatch(page); m != nil {
		rec.Status = strings.Join(strings.Fields(html.UnescapeString(tagRE.ReplaceAllString(m[1], " "))), " ")
	}
	if !strings.HasPrefix(rec.URL, Site+"/") {
		rec.URL = strings.Replace(link, r.Base, Site, 1)
	}
	return rec, nil
}

var (
	// A search result: the company's page (on the site itself) and its name.
	resultRE  = regexp.MustCompile(`<a href="(?:https://dulieuphapluat\.vn)?(/cong-cu/ma-so-thue/[0-9]{10}(?:-[0-9]{3})?-[a-z0-9-]+\.html)" class="tax-company-link">([^<]*)</a>`)
	jsonLDRE  = regexp.MustCompile(`<script[^>]+application/ld\+json[^>]*>([\s\S]*?)</script>`)
	startedRE = regexp.MustCompile(`Ngày bắt đầu hoạt động</td>\s*<td class="tax-detail-value[^"]*">\s*(\d{2})/(\d{2})/(\d{4})`)
	statusRE  = regexp.MustCompile(`<span class="tax-detail-meta-label">Trạng thái</span>\s*<span[^>]*>([\s\S]*?)</span>`)
	tagRE     = regexp.MustCompile(`<[^>]+>`)
	dayRE     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func (r *Register) get(ctx context.Context, link string) (string, error) {
	if err := r.wait(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", r.UserAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := r.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("business register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("business register: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("business register: %w", err)
	}
	return string(b), nil
}

// wait enforces the minimum gap between requests across goroutines.
func (r *Register) wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.Interval - time.Since(r.last); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	r.last = time.Now()
	return nil
}

// The short forms postings use for the words of a company's legal form.
var shortForms = map[string]string{
	"CTY": "CÔNG TY", "CTCP": "CÔNG TY CỔ PHẦN", "CP": "CỔ PHẦN",
	"MTV": "MỘT THÀNH VIÊN", "TM": "THƯƠNG MẠI", "SX": "SẢN XUẤT", "DV": "DỊCH VỤ",
	"XNK": "XUẤT NHẬP KHẨU", "XD": "XÂY DỰNG", "TMDV": "THƯƠNG MẠI DỊCH VỤ",
}

// Key is name as the register would write it, for comparing: one Unicode
// form, capitals, no punctuation, and the short forms written out. Accents
// stay: THIÊN ẤN and THIÊN AN are different companies.
func Key(name string) string {
	s := strings.ToUpper(norm.NFC.String(name))
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`.,;:"'“”‘’()[]&+/\-–—|!?*`, r) {
			return ' '
		}
		return r
	}, s)
	words := strings.Fields(s)
	for i, w := range words {
		if long, ok := shortForms[w]; ok {
			words[i] = long
		}
	}
	return strings.Join(words, " ")
}
