package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/scrape"
)

// Each job source can be turned off: its jobs stop coming onto the board.
// Nothing else changes: a login to the site stays saved, the jobs already on
// the board stay, and a link the user pastes from it is still added (that's
// the user's own choice, not the source's).

// sourcesOffKey is the setting listing the sources turned off (a JSON array).
const sourcesOffKey = "sources_off"

// BoardSource is a job source and whether its jobs come onto the board.
type BoardSource struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	On     bool   `json:"on"`
}

// BoardSources lists the job sites braveduck knows, and any other tool that
// has reported crawls, each with whether it's on.
func (s *Service) BoardSources(ctx context.Context) ([]BoardSource, error) {
	off, err := s.sourcesOff(ctx)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, site := range scrape.Sources {
		ids[site.ID] = true
	}
	for id := range s.builtinSources() {
		ids[id] = true
	}
	if v, err := s.st.CrawlStatus(ctx, s.now(), nil); err == nil {
		for _, src := range v.Sources {
			ids[src.Name] = true
		}
	}
	out := []BoardSource{}
	for id := range ids {
		if isTaskSource(id) {
			continue // rating and TopCV sync aren't sources of jobs
		}
		out = append(out, BoardSource{Source: id, Name: sourceName(id), On: !off[id]})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// SetSourceOn turns a job source on or off for this board.
func (s *Service) SetSourceOn(ctx context.Context, source string, on bool) (BoardSource, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if !crawlSourceRE.MatchString(source) || isTaskSource(source) {
		return BoardSource{}, fmt.Errorf("%w: %q isn't a job source", ErrInvalid, source)
	}
	off, err := s.sourcesOff(ctx)
	if err != nil {
		return BoardSource{}, err
	}
	if off[source] == !on {
		return BoardSource{Source: source, Name: sourceName(source), On: on}, nil
	}
	if on {
		delete(off, source)
	} else {
		off[source] = true
	}
	list := make([]string, 0, len(off))
	for id := range off {
		list = append(list, id)
	}
	slices.Sort(list)
	b, _ := json.Marshal(list)
	if err := s.st.SetSetting(ctx, sourcesOffKey, string(b)); err != nil {
		return BoardSource{}, err
	}
	if on {
		s.logEvent(ctx, "info", sourceName(source)+": its jobs come onto the board again")
	} else {
		s.logEvent(ctx, "info", sourceName(source)+": turned off, its jobs no longer come onto the board")
	}
	return BoardSource{Source: source, Name: sourceName(source), On: on}, nil
}

// SourceOn reports whether source's jobs come onto the board.
func (s *Service) SourceOn(ctx context.Context, source string) bool {
	off, err := s.sourcesOff(ctx)
	return err != nil || !off[strings.ToLower(source)] // can't tell: don't drop jobs
}

func (s *Service) sourcesOff(ctx context.Context) (map[string]bool, error) {
	v, ok, err := s.st.Setting(ctx, sourcesOffKey)
	if err != nil || !ok {
		return map[string]bool{}, err
	}
	var list []string
	if err := json.Unmarshal([]byte(v), &list); err != nil {
		return map[string]bool{}, nil // a broken setting turns nothing off
	}
	off := map[string]bool{}
	for _, id := range list {
		off[id] = true
	}
	return off, nil
}

// errSourceOff says why a job from a source that's off wasn't added.
func errSourceOff(source string) error {
	return fmt.Errorf("%w: %s is turned off on this board — turn it on in Job sources to take its jobs", ErrInvalid, sourceName(source))
}
