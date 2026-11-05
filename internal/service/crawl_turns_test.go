package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/crawl"
)

// slowSite counts how many searches run against it at once.
type slowSite struct {
	now, peak atomic.Int32
}

func (f *slowSite) Name() string { return "careerlink" }

func (f *slowSite) Search(context.Context, string) ([]crawl.Listing, error) {
	n := f.now.Add(1)
	defer f.now.Add(-1)
	for p := f.peak.Load(); n > p && !f.peak.CompareAndSwap(p, n); p = f.peak.Load() {
	}
	time.Sleep(30 * time.Millisecond)
	return nil, nil
}

// Two users' services crawling the same site take turns: the site never
// sees two braveduck crawls at once. A crawl waiting for its turn gives up
// when its context ends.
func TestCrawlsOfOneSiteTakeTurns(t *testing.T) {
	site := &slowSite{}
	var svcs []*Service
	for range 3 {
		s := newSvc(t)
		s.SetCrawlers(site)
		addTerms(t, s, "search_queries", "n8n")
		svcs = append(svcs, s)
	}
	var wg sync.WaitGroup
	for _, s := range svcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Crawl(context.Background(), "careerlink"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p := site.peak.Load(); p != 1 {
		t.Fatalf("%d crawls of one site ran at once, want 1", p)
	}

	release, err := takeSiteTurn(context.Background(), "careerlink")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := svcs[0].Crawl(ctx, "careerlink"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting crawl = %v, want its deadline", err)
	}
}
