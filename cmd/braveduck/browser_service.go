package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
)

// cmdBrowserService runs the shared server's browser service (its own
// container in deploy/hosted): browsers for braveduck, behind a key.
func cmdBrowserService(args []string) error {
	fs := flag.NewFlagSet("browser-service", flag.ContinueOnError)
	addr := fs.String("addr", ":7317", "listen address")
	bin := fs.String("browser", "/usr/bin/chrome", "the Chromium to run")
	keyFile := fs.String("key-file", "", "file holding the key braveduck sends (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyFile == "" {
		return errors.New("browser-service needs --key-file")
	}
	b, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(string(b))
	if len(key) < 32 {
		return errors.New("the browser service key must be at least 32 characters")
	}
	srv := &http.Server{Addr: *addr, Handler: browser.Service(*bin, key), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Printf("braveduck browser service on %s\n", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
