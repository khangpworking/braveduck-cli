// Package keyword holds the portable crawler keyword-filter logic: the named
// list model, diacritic-insensitive term normalization, and the pass/reject
// decision a crawler applies to each discovered job.
//
// Faithful port of the proven logic in the original personal-assistant app
// (src/lib/keywordLists.ts). The list *keys* and matching semantics are carried
// over verbatim — they encode real filtering meaning (title-match vs
// title-reject vs content-reject vs company-reject) any user wants. What is NOT
// carried over is the author's personal term seed: braveduck-cli ships every
// list EMPTY, so the tool never inherits someone else's job preferences.
package keyword

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Target is what a list's terms are matched against.
type Target int

const (
	TargetFetch   Target = iota // search terms crawlers fetch — not a filter
	TargetTitle                 // matched against the job title
	TargetContent               // matched against title OR full description
	TargetCompany               // matched against the company name
)

// List describes one managed keyword list and how it participates in filtering.
type List struct {
	Key    string
	Label  string
	Folded bool   // fold diacritics on both terms and haystack before matching
	Target Target // what terms match against
	Reject bool   // true: a match REJECTS the job; false: positive/fetch list
}

// Lists is the fixed set of managed lists, in editor/display order. This mirrors
// KEYWORD_LISTS in the original app. Only the four filter lists feed Filter;
// search_queries feeds the (browser-backed) crawlers.
var Lists = []List{
	{"search_queries", "Search queries (terms crawlers fetch)", false, TargetFetch, false},
	{"positive_title", "Title must match one of these", false, TargetTitle, false},
	{"negative_title", "Reject if in title", true, TargetTitle, true},
	{"negative_content", "Reject if in title or description", true, TargetContent, true},
	{"negative_company", "Reject if in company name", true, TargetCompany, true},
}

// ListByKey returns the metadata for a list key, or false if unknown.
func ListByKey(key string) (List, bool) {
	for _, l := range Lists {
		if l.Key == key {
			return l, true
		}
	}
	return List{}, false
}

// Keys returns every managed list key, in display order.
func Keys() []string {
	out := make([]string, len(Lists))
	for i, l := range Lists {
		out[i] = l.Key
	}
	return out
}

// Fold strips diacritics (Unicode NFD minus combining marks) and lowercases,
// so "Thẩm Mỹ" and "tham my" match. đ/Đ don't decompose under NFD, so they're
// mapped explicitly — matching the original foldDiacritics exactly.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r): // nonspacing combining mark
			continue
		case r == 'đ' || r == 'Đ':
			b.WriteRune('d')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeTerm cleans one raw term the way the matchers expect: trimmed,
// lowercased, and diacritic-folded when the target list is folded. Empty result
// means the term was blank.
func NormalizeTerm(term string, folded bool) string {
	t := strings.ToLower(strings.TrimSpace(term))
	if folded {
		t = Fold(t)
	}
	return t
}

// haystack lowercases (and folds when needed) the text a term is matched in.
func haystack(s string, folded bool) string {
	s = strings.ToLower(s)
	if folded {
		s = Fold(s)
	}
	return s
}

// Job is the minimal view Filter needs — decoupled from the full store record so
// the crawler can filter a freshly-scraped listing before it's ever persisted.
type Job struct {
	Title       string
	Description string
	Company     string
}

// Decision is the outcome of applying the filter to a job.
type Decision struct {
	Keep   bool
	List   string // the list key responsible for the decision ("" if kept with no positive list)
	Term   string // the matched/failing term
	Reason string // human-readable explanation
}

// Sets maps each list key to its raw (unnormalized) terms — the shape the store
// hands back. Filter normalizes per-list, so callers pass terms as stored.
type Sets map[string][]string

// Filter decides whether a job survives the keyword lists, in the original's
// order of precedence:
//  1. positive_title — if non-empty, the title must contain at least one term;
//  2. negative_title — reject if the title contains any term;
//  3. negative_content — reject if title OR description contains any term;
//  4. negative_company — reject if the company contains any term.
// An empty positive_title list means "no positive gate" (everything passes 1).
func Filter(j Job, sets Sets) Decision {
	// 1. Positive title gate.
	if terms := normalizedFor("positive_title", sets); len(terms) > 0 {
		title := haystack(j.Title, listFolded("positive_title"))
		if !containsAny(title, terms) {
			return Decision{Keep: false, List: "positive_title", Reason: "title matched no required keyword"}
		}
	}

	// 2-4. Negative gates.
	for _, key := range []string{"negative_title", "negative_content", "negative_company"} {
		terms := normalizedFor(key, sets)
		if len(terms) == 0 {
			continue
		}
		text := targetText(j, mustList(key).Target, listFolded(key))
		if term, ok := firstMatch(text, terms); ok {
			return Decision{Keep: false, List: key, Term: term, Reason: "matched negative keyword \"" + term + "\""}
		}
	}
	return Decision{Keep: true}
}

func targetText(j Job, t Target, folded bool) string {
	switch t {
	case TargetTitle:
		return haystack(j.Title, folded)
	case TargetContent:
		return haystack(j.Title+" "+j.Description, folded)
	case TargetCompany:
		return haystack(j.Company, folded)
	default:
		return ""
	}
}

func normalizedFor(key string, sets Sets) []string {
	folded := listFolded(key)
	var out []string
	for _, raw := range sets[key] {
		if t := NormalizeTerm(raw, folded); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func listFolded(key string) bool { return mustList(key).Folded }

func mustList(key string) List {
	l, ok := ListByKey(key)
	if !ok {
		panic("keyword: unknown list " + key)
	}
	return l
}

func containsAny(hay string, terms []string) bool {
	_, ok := firstMatch(hay, terms)
	return ok
}

func firstMatch(hay string, terms []string) (string, bool) {
	for _, t := range terms {
		if strings.Contains(hay, t) {
			return t, true
		}
	}
	return "", false
}
