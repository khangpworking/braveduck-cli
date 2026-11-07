package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/fbwatcher"
	"github.com/khangpworking/braveduck-cli/internal/hosted"
)

// isHostedServe reports whether args (after "serve") ask for a shared server.
func isHostedServe(args []string) bool {
	for _, a := range args {
		if a == "--users" || strings.HasPrefix(a, "--users=") || a == "-users" || strings.HasPrefix(a, "-users=") {
			return true
		}
	}
	return false
}

// cmdServeHosted runs a shared server: one board per user, behind a login
// proxy (deploy/hosted has Caddy + Authelia ready to use).
func cmdServeHosted(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	users := fs.String("users", "", "directory holding one data dir per user (turns on the shared server)")
	addr := fs.String("addr", "127.0.0.1:8080", "listen address (host:port); only the proxy should reach it")
	header := fs.String("user-header", "Remote-User", "header in which the login proxy names the logged-in user")
	secretFile := fs.String("proxy-secret-file", "", "file holding the secret the proxy sends in "+hosted.SecretHeader+" (default: $BRAVEDUCK_PROXY_SECRET)")
	maxUsers := fs.Int("max-users", 50, "most users the server holds")
	maxBrowsers := fs.Int("max-browsers", 2, "most browsers (crawls, page fetches, PDFs) running at once, for all users together")
	logout := fs.String("logout-url", "", "the login service's log-out page, linked from the board")
	scannerAddr := fs.String("scanner-addr", "", "listen address for the Facebook scanner (fb-watcher); off when empty — never expose it publicly")
	scannerKeyFile := fs.String("scanner-key-file", "", "file holding the scanner's key (required with --scanner-addr)")
	usersFile := fs.String("login-users-file", "", "the login service's users file (Authelia's users_database.yml): agent keys of users removed or disabled there stop working")
	maxJobs := fs.Int("max-jobs-per-user", 5000, "jobs each user's board holds (0 = no limit)")
	jevDaily := fs.Int("jev-daily-limit", 50, "Jev ratings per user per day, paid by the server's key (0 = no limit)")
	accessTeam := fs.String("access-team", "", "log in with Cloudflare Access instead of a proxy: the team's address, https://TEAM.cloudflareaccess.com (see deploy/cloudflare)")
	accessAUD := fs.String("access-aud", "", "with --access-team: the Access application's audience (AUD) tag")
	owner := fs.String("owner", "", "the user (login name or email) whose computer this is: job-site logins and assisted apply stay on for them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	secret := os.Getenv("BRAVEDUCK_PROXY_SECRET")
	if *secretFile != "" {
		b, err := os.ReadFile(*secretFile)
		if err != nil {
			return fmt.Errorf("read the proxy secret: %w", err)
		}
		secret = strings.TrimSpace(string(b))
	}
	viaAccess := *accessTeam != "" || *accessAUD != ""
	if secret == "" && !viaAccess {
		return errors.New("a shared server needs the proxy secret (--proxy-secret-file FILE or $BRAVEDUCK_PROXY_SECRET), or Cloudflare Access (--access-team and --access-aud)")
	}
	browser.SetMaxBrowsers(*maxBrowsers)
	fbw, err := fbwatcher.FromEnv()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	h, err := hosted.New(srvCtx, hosted.Options{
		Dir: *users, UserHeader: *header, Secret: secret,
		MaxUsers: *maxUsers, LogoutURL: *logout, JevDaily: *jevDaily, MaxJobs: *maxJobs, UsersFile: *usersFile, Tick: time.Minute,
		AccessTeam: *accessTeam, AccessAUD: *accessAUD, FBWatcher: fbw, Owner: *owner,
	})
	if err != nil {
		return err
	}
	defer func() { cancel(); h.Close() }()

	if *scannerAddr != "" {
		if err := serveScanner(ctx, h, *scannerAddr, *scannerKeyFile); err != nil {
			return err
		}
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		sc, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sc)
	}()
	login := "behind its login proxy"
	if viaAccess {
		login = "logins by Cloudflare Access"
	}
	fmt.Printf("braveduck shared server on %s for %d user(s) in %s (%s)\n", ln.Addr(), len(h.Users()), *users, login)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// serveScanner starts the Facebook scanner's API on its own listener.
func serveScanner(ctx context.Context, h *hosted.Server, addr, keyFile string) error {
	if keyFile == "" {
		return errors.New("--scanner-addr needs --scanner-key-file")
	}
	b, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("read the scanner key: %w", err)
	}
	key := strings.TrimSpace(string(b))
	if len(key) < 32 {
		return errors.New("the scanner key must be at least 32 characters")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	srv := &http.Server{Handler: h.Scanner(key), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() { _ = srv.Serve(ln) }()
	fmt.Printf("Facebook scanner API on %s\n", ln.Addr())
	return nil
}
