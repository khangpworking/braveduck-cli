package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/api"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// cmdServe boots the local web board + JSON API and (unless --no-open) opens the
// user's browser at it. This is the "install and a board pops up" entry point.
func cmdServe(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address (host:port)")
	noOpen := fs.Bool("no-open", false, "don't auto-open the browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Ctrl-C triggers a graceful shutdown.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	// Bind first so we can report the real URL (and fail fast on a busy port).
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	url := "http://" + ln.Addr().String()

	srv := &http.Server{Handler: api.Handler(st)}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()

	fmt.Printf("braveduck board on %s  (Ctrl-C to stop)\n", url)
	if !*noOpen {
		openBrowser(url)
	}

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
