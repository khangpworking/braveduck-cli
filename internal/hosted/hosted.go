// Package hosted runs braveduck as a shared server: many users, each with
// their own board, behind a login proxy.
//
// braveduck does no logins itself. A reverse proxy (Caddy) asks a login
// service (Authelia) about every request and passes the logged-in user's
// name on in a header; deploy/hosted has a ready configuration. Server
// trusts that header only on requests that also carry the secret shared
// with the proxy, so nothing that reaches braveduck's port some other way
// can pose as a user.
//
// Each user gets a data dir of their own (DIR/<user>/: the database, the
// profile, CVs, backups and the session key) and a complete single-user
// braveduck over it: a service with its scheduler, and the same board and
// API as on a laptop, with its own per-run token. Users never share data.
package hosted

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/api"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// SecretHeader carries the secret shared with the proxy.
const SecretHeader = "X-Braveduck-Proxy-Secret"

// Options configures a Server.
type Options struct {
	Dir        string // holds one data dir per user
	UserHeader string // the proxy's logged-in-user header, e.g. Remote-User
	Secret     string // shared with the proxy; at least 32 characters
	MaxUsers   int    // data dirs the server will hold; new users past it are refused
	LogoutURL  string // shown on the board; "" hides the link

	// Tick is how often each user's scheduler looks for due work (crawls,
	// rating, sweeps, backups); 0 turns schedulers off (tests).
	Tick time.Duration
}

// Server routes each request to its user's board.
type Server struct {
	opts Options
	ctx  context.Context // ends every user's scheduler

	mu    sync.Mutex
	users map[string]*tenant
}

type tenant struct {
	st      *store.Store
	svc     *service.Service
	handler http.Handler
	stopped chan struct{} // closed when the scheduler has returned
}

// validUser is what a user name may look like once lower-cased: it is used
// as a directory name, so no slashes and no leading dot.
var validUser = regexp.MustCompile(`^[a-z0-9][a-z0-9._@-]{0,63}$`)

// New checks opts and opens the board of every user who already has a data
// dir, so their crawls keep running after a restart even before they visit.
// Cancelling ctx stops every scheduler; Close then closes the databases.
func New(ctx context.Context, opts Options) (*Server, error) {
	switch {
	case opts.Dir == "":
		return nil, errors.New("hosted: no users directory")
	case opts.UserHeader == "":
		return nil, errors.New("hosted: no user header")
	case len(opts.Secret) < 32:
		return nil, errors.New("hosted: the proxy secret must be at least 32 characters")
	case opts.MaxUsers <= 0:
		return nil, errors.New("hosted: max users must be at least 1")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(opts.Dir, 0o700) // best effort: it may predate braveduck
	s := &Server{opts: opts, ctx: ctx, users: map[string]*tenant{}}
	entries, err := os.ReadDir(opts.Dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !validUser.MatchString(e.Name()) {
			continue
		}
		if _, err := s.user(e.Name()); err != nil {
			s.Close()
			return nil, fmt.Errorf("hosted: open %s: %w", e.Name(), err)
		}
	}
	return s, nil
}

// Users lists the users with an open board, sorted.
func (s *Server) Users() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.users))
	for u := range s.users {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// ServeHTTP checks the proxy's secret, then hands the request to the board
// of the user the proxy says is logged in.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	got := r.Header.Get(SecretHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Secret)) != 1 {
		writeErr(w, http.StatusForbidden, "braveduck only answers through its login proxy")
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.Header.Get(s.opts.UserHeader)))
	if name == "" {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	if !validUser.MatchString(name) {
		writeErr(w, http.StatusForbidden, "this user name can't be used on braveduck")
		return
	}
	t, err := s.user(name)
	switch {
	case errors.Is(err, errFull):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "can't open your board: "+err.Error())
		return
	}

	// The user's board is the single-user one: it only answers requests for
	// localhost, which is true here (the proxy is the only way in), and it
	// must never see the proxy's headers.
	r2 := r.Clone(r.Context())
	r2.Host = "localhost"
	r2.Header.Del(SecretHeader)
	r2.Header.Del(s.opts.UserHeader)
	t.handler.ServeHTTP(w, r2)
}

var errFull = errors.New("this braveduck server has no room for another user — ask its admin")

// user returns name's board, opening it (and creating its data dir) on
// first use.
func (s *Server) user(name string) (*tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.users[name]; ok {
		return t, nil
	}
	if len(s.users) >= s.opts.MaxUsers {
		return nil, errFull
	}
	t, err := s.open(name)
	if err != nil {
		return nil, err
	}
	s.users[name] = t
	return t, nil
}

func (s *Server) open(name string) (*tenant, error) {
	dir := filepath.Join(s.opts.Dir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db := filepath.Join(dir, "braveduck.db")
	st, err := store.Open(db)
	if err != nil {
		return nil, err
	}
	for _, f := range []string{db, db + "-wal", db + "-shm"} {
		_ = os.Chmod(f, 0o600) // best effort; missing -wal/-shm is fine
	}
	svc := service.New(st)
	svc.SetDataDir(dir)
	svc.SetHosted(service.HostedInfo{User: name, LogoutURL: s.opts.LogoutURL})

	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		st.Close()
		return nil, err
	}
	t := &tenant{st: st, svc: svc, handler: api.Handler(st, svc, hex.EncodeToString(tok)), stopped: make(chan struct{})}
	if s.opts.Tick <= 0 {
		close(t.stopped)
		return t, nil
	}
	go func() {
		defer close(t.stopped)
		// Start at a random point in the first tick, so a restart doesn't
		// run every user's due crawls in the same second.
		if n, err := rand.Int(rand.Reader, big.NewInt(int64(s.opts.Tick))); err == nil {
			select {
			case <-time.After(time.Duration(n.Int64())):
			case <-s.ctx.Done():
				return
			}
		}
		svc.RunScheduler(s.ctx, s.opts.Tick)
	}()
	return t, nil
}

// Close waits for the schedulers (cancel New's ctx first) and closes every
// user's database.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, t := range s.users {
		<-t.stopped
		t.st.Close()
		delete(s.users, name)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
