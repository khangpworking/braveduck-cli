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
	jevDaily := fs.Int("jev-daily-limit", 50, "Jev ratings per user per day, paid by the server's key (0 = no limit)")
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
	if secret == "" {
		return errors.New("a shared server needs the proxy secret: --proxy-secret-file FILE or $BRAVEDUCK_PROXY_SECRET")
	}
	browser.SetMaxBrowsers(*maxBrowsers)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	h, err := hosted.New(srvCtx, hosted.Options{
		Dir: *users, UserHeader: *header, Secret: secret,
		MaxUsers: *maxUsers, LogoutURL: *logout, JevDaily: *jevDaily, Tick: time.Minute,
	})
	if err != nil {
		return err
	}
	defer func() { cancel(); h.Close() }()

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
	fmt.Printf("braveduck shared server on %s for %d user(s) in %s (behind its login proxy)\n", ln.Addr(), len(h.Users()), *users)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
