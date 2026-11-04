package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/jobs"
	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
)

// A job that's already on the board is never added again, however it comes
// in: a paste, `add`, a crawl or the TopCV sync. Two jobs are the same listing
// when they share a link (in canonical form, so links stored with tracking
// parts still match), a title at the same company, or their job text. Blank
// fields never match.

// DuplicateError is a job that's already on the board; Jobs are the matches.
// It matches ErrDuplicate.
type DuplicateError struct{ Jobs []jobs.Job }

func (e *DuplicateError) Error() string {
	var names []string
	for i, j := range e.Jobs {
		if i == 3 {
			names = append(names, fmt.Sprintf("and %d more", len(e.Jobs)-i))
			break
		}
		names = append(names, fmt.Sprintf("#%d %q (%s)", j.ID, j.Title, j.Status))
	}
	return ErrDuplicate.Error() + ": " + strings.Join(names, ", ")
}

func (e *DuplicateError) Is(target error) bool { return target == ErrDuplicate }

// minDupText is the shortest job text that counts as a match on its own; a
// one-line paste is too generic to call two jobs the same.
const minDupText = 50

// dupIndex is the board's jobs keyed the three ways they can match. A crawl
// builds it once and adds each new job, so it costs one board read per run.
type dupIndex struct {
	keys map[string][]jobs.Job
}

func (s *Service) loadDupIndex(ctx context.Context) (*dupIndex, error) {
	all, err := s.st.List(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	x := &dupIndex{keys: map[string][]jobs.Job{}}
	for _, j := range all {
		x.add(j)
	}
	return x, nil
}

// dupKeys are the keys a job can be matched on.
func dupKeys(j jobs.Job) []string {
	var keys []string
	if u := strings.TrimSpace(j.URL); u != "" {
		if _, c, ok := scrape.SupportedLink(u); ok {
			u = c
		}
		keys = append(keys, "url\x00"+u)
	}
	if t, c := fold(j.Title), fold(j.Company); t != "" && c != "" {
		keys = append(keys, "title\x00"+t+"\x00"+c)
	}
	if t := fold(postText(j.FullJD)); len(t) >= minDupText {
		keys = append(keys, "text\x00"+t)
	}
	return keys
}

func (x *dupIndex) add(j jobs.Job) {
	for _, k := range dupKeys(j) {
		x.keys[k] = append(x.keys[k], j)
	}
}

// find returns the jobs j duplicates, each once, or nil.
func (x *dupIndex) find(j jobs.Job) []jobs.Job {
	var out []jobs.Job
	seen := map[int64]bool{}
	for _, k := range dupKeys(j) {
		for _, o := range x.keys[k] {
			if !seen[o.ID] {
				seen[o.ID] = true
				out = append(out, o)
			}
		}
	}
	return out
}

// rejectDuplicates returns a *DuplicateError when j is already on the board.
func (s *Service) rejectDuplicates(ctx context.Context, j jobs.Job) error {
	x, err := s.loadDupIndex(ctx)
	if err != nil {
		return err
	}
	if dups := x.find(j); len(dups) > 0 {
		return &DuplicateError{Jobs: dups}
	}
	return nil
}

// postText is a job's text without a trailing footer after a "---" line
// (fb-watcher appends the author, group and matched keywords there), so the
// same post compares equal however it was added.
func postText(jd string) string {
	jd = strings.ReplaceAll(jd, "\r\n", "\n")
	if i := strings.Index(jd, "\n---\n"); i >= 0 {
		return jd[:i]
	}
	return strings.TrimSuffix(jd, "\n---")
}

// fold lowercases and collapses whitespace, for comparing text.
func fold(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
