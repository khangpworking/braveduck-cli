package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/api"
	"github.com/khangpworking/braveduck-cli/internal/core/company"
	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// cmdServe boots the local web board + JSON API and (unless --no-open) opens the
// user's browser at it. This is the "install and a board pops up" entry point.
func cmdServe(ctx context.Context, st *store.Store, svc *service.Service, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address (host:port)")
	noOpen := fs.Bool("no-open", false, "don't auto-open the browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Ctrl-C triggers a graceful shutdown.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	// The board trusts whoever can load it (the page carries the API token),
	// so it only listens on this machine. Several people: serve --users.
	if host, _, err := net.SplitHostPort(*addr); err != nil || !isLoopbackListen(host) {
		return fmt.Errorf("--addr %s: the board only listens on this machine (127.0.0.1 or localhost); to serve several people, see `serve --users` and deploy/hosted", *addr)
	}

	// Bind first so we can report the real URL (and fail fast on a busy port).
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	url := "http://" + ln.Addr().String()

	// Per-run API token (ARCHITECTURE.md §1.3): injected into the board page, so
	// the user never handles it, and required on every /api/ call.
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		ln.Close()
		return err
	}

	srv := &http.Server{
		Handler:           api.Handler(st, svc, hex.EncodeToString(tok)),
		ReadHeaderTimeout: 10 * time.Second,
		// Requests inherit ctx, so Ctrl-C also ends long-lived event streams
		// instead of Shutdown waiting on them.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()

	// Built-in crawlers run on their schedules while the board is up, and
	// the jobs' companies are looked up in the business register.
	svc.SetCompanyRegister(company.New())
	go svc.RunScheduler(ctx, time.Minute)

	fmt.Printf("braveduck board on %s  (Ctrl-C to stop)\n", url)
	if !*noOpen {
		openBrowser(url)
	}

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// isLoopbackListen reports whether a listen host is this machine only.
func isLoopbackListen(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// openBrowser best-effort launches the OS default browser; failures are silent
// (headless boxes, no GUI) — the URL was already printed.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
}
