package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/store"
)

// watchEvery is how often the shared watcher polls the DB; overridable in tests.
var watchEvery = time.Second

// keepAliveEvery keeps idle SSE connections from being dropped by anything in
// between, and lets the server notice a client that has gone away.
var keepAliveEvery = 25 * time.Second

// hub fans one store watcher out to every connected board. The watcher runs
// only while at least one board is listening.
type hub struct {
	st *store.Store

	mu     sync.Mutex
	subs   map[chan struct{}]struct{}
	cancel context.CancelFunc
}

func newHub(st *store.Store) *hub {
	return &hub{st: st, subs: map[chan struct{}]struct{}{}}
}

// subscribe returns a channel that receives a value after each DB change, and
// a func to stop listening.
func (h *hub) subscribe() (<-chan struct{}, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) == 0 {
		ctx, cancel := context.WithCancel(context.Background())
		src, err := h.st.Watch(ctx, watchEvery)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		h.cancel = cancel
		go h.fan(src)
	}
	ch := make(chan struct{}, 1)
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs, ch)
		if len(h.subs) == 0 && h.cancel != nil {
			h.cancel()
			h.cancel = nil
		}
	}, nil
}

func (h *hub) fan(src <-chan struct{}) {
	for range src {
		h.mu.Lock()
		for ch := range h.subs {
			select {
			case ch <- struct{}{}:
			default: // that board already has a change pending
			}
		}
		h.mu.Unlock()
	}
}

// events is GET /api/events: a Server-Sent Events stream with one "changed"
// event whenever the board's data changes, whoever changed it (the board,
// the CLI, an agent over MCP). The board refetches on each event.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	ch, unsubscribe, err := s.hub.subscribe()
	if errors.Is(err, store.ErrWatchUnsupported) {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()

	ping := time.NewTicker(keepAliveEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: changed\ndata: {}\n\n")
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}
