// Package company checks exact legal names on dulieuphapluat.vn, using the
// workplace province to resolve duplicate names and confirm English aliases.
// Redirects are verified, every request is paced 2 s apart, and each lookup
// uses at most five requests; unresolved names keep a link to the site's search.
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

var errNameMismatch = errors.New("company page: name does not match the lookup")

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
	seen   map[string]result // answers by LookupKey, shared by every board on this server
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

// Find looks up a name, using the workplace province when the name is ambiguous.
func (r *Register) Find(ctx context.Context, name, location string) (Record, error) {
	key := LookupKey(name, location)
	if Key(name) == "" {
		return Record{}, ErrNotFound
	}
	r.seenMu.Lock()
	res, ok := r.seen[key]
	r.seenMu.Unlock()
	if ok {
		return res.rec, res.err
	}
	rec, err := r.find(ctx, name, Province(location))
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

const searchPath = "/cong-cu/tra-cuu-ma-so-thue.html"
const maxRequests = 5

func (r *Register) find(ctx context.Context, name, province string) (Record, error) {
	budget := maxRequests
	key := Key(name)
	page, err := r.get(ctx, r.Base+searchPath+"?keyword="+url.QueryEscape(key), &budget)
	if err != nil {
		return Record{}, err
	}
	if companyURLRE.MatchString(page.url.Path) {
		return acceptPage(page, key, province, false)
	}
	hits, err := searchHits(page.body, func(n string) bool { return n == key })
	if err != nil {
		return Record{}, err
	}
	if len(hits) == 1 {
		page, err := r.get(ctx, r.Base+hits[0], &budget)
		if err != nil {
			return Record{}, err
		}
		return acceptPage(page, key, province, false)
	}
	if len(hits) > 1 {
		// Never pick from a partial set of same-name companies.
		if province == "" || len(hits) > budget {
			return Record{}, ErrNotFound
		}
		var matches []Record
		for _, hit := range hits {
			page, err := r.get(ctx, r.Base+hit, &budget)
			if err != nil {
				return Record{}, err
			}
			rec, err := acceptPage(page, key, province, true)
			if err == nil {
				matches = append(matches, rec)
			} else if !errors.Is(err, ErrNotFound) {
				return Record{}, err
			}
		}
		return unique(matches)
	}

	distinctive := englishName(key)
	if distinctive == "" || province == "" {
		return Record{}, ErrNotFound
	}
	page, err = r.get(ctx, r.Base+searchPath+"?keyword="+url.QueryEscape(distinctive), &budget)
	if err != nil {
		return Record{}, err
	}
	if companyURLRE.MatchString(page.url.Path) {
		return acceptPage(page, key, province, true)
	}
	hits, err = searchHits(page.body, func(n string) bool {
		for _, word := range strings.Fields(distinctive) {
			if !strings.Contains(" "+n+" ", " "+word+" ") {
				return false
			}
		}
		return true
	})
	if err != nil {
		return Record{}, err
	}
	var matches []Record
	var mismatch error
	for _, hit := range hits[:min(len(hits), 3)] {
		page, err := r.get(ctx, r.Base+hit, &budget)
		if err != nil {
			return Record{}, err
		}
		rec, err := acceptPage(page, key, province, true)
		if err == nil {
			matches = append(matches, rec)
		} else if errors.Is(err, errNameMismatch) {
			mismatch = err
		} else if !errors.Is(err, ErrNotFound) {
			return Record{}, err
		}
	}
	if len(matches) == 0 && mismatch != nil {
		return Record{}, mismatch
	}
	return unique(matches)
}

func unique(matches []Record) (Record, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	return Record{}, ErrNotFound
}

func searchHits(body string, match func(string) bool) ([]string, error) {
	if !strings.Contains(body, "tax-result-list") && !strings.Contains(body, "tax-search-result-summary") {
		return nil, errors.New("company search: the page has changed")
	}
	var hits []string
	for _, m := range resultRE.FindAllStringSubmatch(body, -1) {
		if match(Key(html.UnescapeString(m[2]))) && !slices.Contains(hits, m[1]) {
			hits = append(hits, m[1])
		}
	}
	return hits, nil
}

// English legal forms are stripped only for discovery, never for identity.
func englishName(key string) string {
	for _, suffix := range []string{"JOINT STOCK COMPANY", "COMPANY LIMITED", "CO LTD", "JSC", "CORPORATION"} {
		if s, ok := strings.CutSuffix(key, " "+suffix); ok {
			return s
		}
	}
	return ""
}

func acceptPage(page fetchedPage, key, province string, requireProvince bool) (Record, error) {
	rec, alternate, registeredProvince, err := parseCompany(page)
	if err != nil {
		return Record{}, err
	}
	if Key(rec.Name) != key {
		if englishName(key) == "" || Key(alternate) != key {
			return Record{}, errNameMismatch
		}
		requireProvince = true
	}
	if (requireProvince || englishName(key) != "") && (province == "" || registeredProvince != province) {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

// Closed reports whether the register's status says the company has
// stopped operating ("NNT ngừng hoạt động và đã đóng MST", "NNT tạm ngừng
// hoạt động", …).
func Closed(status string) bool {
	s := strings.ToLower(status)
	return strings.Contains(s, "ngừng hoạt động") || strings.Contains(s, "đóng mst")
}

// parseCompany reads only company metadata, never representative details.
func parseCompany(page fetchedPage) (Record, string, string, error) {
	var rec Record
	var alternate, province string
	for _, m := range jsonLDRE.FindAllStringSubmatch(page.body, -1) {
		var o struct {
			Type          string `json:"@type"`
			Name          string `json:"name"`
			LegalName     string `json:"legalName"`
			AlternateName string `json:"alternateName"`
			TaxID         string `json:"taxID"`
			FoundingDate  string `json:"foundingDate"`
			Address       struct {
				StreetAddress string `json:"streetAddress"`
			} `json:"address"`
		}
		if json.Unmarshal([]byte(m[1]), &o) == nil && o.Type == "Organization" && o.TaxID != "" {
			rec = Record{TaxCode: o.TaxID, Name: cmp.Or(o.LegalName, o.Name), Founded: o.FoundingDate}
			alternate, province = o.AlternateName, Province(o.Address.StreetAddress)
			break
		}
	}
	if rec.TaxCode == "" {
		return Record{}, "", "", errors.New("company page: no company data on it (the page has changed)")
	}
	if m := companyURLRE.FindStringSubmatch(page.url.Path); m == nil || m[1] != rec.TaxCode {
		return Record{}, "", "", errors.New("company page: tax ID does not match its URL")
	}
	if rec.Founded == "" {
		if m := startedRE.FindStringSubmatch(page.body); m != nil {
			rec.Founded = m[3] + "-" + m[2] + "-" + m[1]
		}
	}
	if rec.Founded != "" {
		if _, err := time.Parse("2006-01-02", rec.Founded); err != nil {
			return Record{}, "", "", fmt.Errorf("company page: invalid founding date: %w", err)
		}
	}
	if m := statusRE.FindStringSubmatch(page.body); m != nil {
		rec.Status = strings.Join(strings.Fields(html.UnescapeString(tagRE.ReplaceAllString(m[1], " "))), " ")
	}
	rec.URL = Site + page.url.Path
	return rec, alternate, province, nil
}

var (
	// A search result: the company's page (on the site itself) and its name.
	resultRE     = regexp.MustCompile(`<a href="(?:https://dulieuphapluat\.vn)?(/cong-cu/ma-so-thue/[0-9]{10}(?:-[0-9]{3})?-[a-z0-9-]+\.html)" class="tax-company-link">([^<]*)</a>`)
	jsonLDRE     = regexp.MustCompile(`<script[^>]+application/ld\+json[^>]*>([\s\S]*?)</script>`)
	startedRE    = regexp.MustCompile(`Ngày bắt đầu hoạt động</td>\s*<td class="tax-detail-value[^"]*">\s*(\d{2})/(\d{2})/(\d{4})`)
	statusRE     = regexp.MustCompile(`<span class="tax-detail-meta-label">Trạng thái</span>\s*<span[^>]*>([\s\S]*?)</span>`)
	tagRE        = regexp.MustCompile(`<[^>]+>`)
	companyURLRE = regexp.MustCompile(`^/cong-cu/ma-so-thue/([0-9]{10}(?:-[0-9]{3})?)-[a-z0-9-]+\.html$`)
)

type fetchedPage struct {
	url  *url.URL
	body string
}

func (r *Register) get(ctx context.Context, link string, budget *int) (fetchedPage, error) {
	base, err := url.Parse(r.Base)
	if err != nil {
		return fetchedPage{}, err
	}
	u, err := url.Parse(link)
	if err != nil {
		return fetchedPage{}, err
	}
	client := *r.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for hops := 0; ; hops++ {
		if u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil ||
			(u.Path != searchPath && !companyURLRE.MatchString(u.Path)) {
			return fetchedPage{}, errors.New("business register: unsafe redirect URL")
		}
		if *budget == 0 {
			return fetchedPage{}, errors.New("business register: lookup request budget exhausted")
		}
		if err := r.wait(ctx); err != nil {
			return fetchedPage{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return fetchedPage{}, err
		}
		req.Header.Set("User-Agent", r.UserAgent)
		req.Header.Set("Accept", "text/html")
		*budget--
		resp, err := client.Do(req)
		if err != nil {
			return fetchedPage{}, fmt.Errorf("business register: %w", err)
		}
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			resp.Body.Close()
			if hops >= 2 {
				return fetchedPage{}, errors.New("business register: too many redirects")
			}
			u, err = resp.Location()
			if err != nil {
				return fetchedPage{}, fmt.Errorf("business register: %w", err)
			}
			continue
		case http.StatusOK:
			b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			if err != nil {
				return fetchedPage{}, fmt.Errorf("business register: %w", err)
			}
			return fetchedPage{u, string(b)}, nil
		default:
			resp.Body.Close()
			return fetchedPage{}, fmt.Errorf("business register: HTTP %d", resp.StatusCode)
		}
	}
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
