package browser

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

// launchOpts configures one browser launch.
type launchOpts struct {
	Headless bool
	// Seed pins CloakBrowser's fingerprint. Empty = a fresh random one, which
	// is right for anonymous fetching; a logged-in account must always present
	// the same seed, or it looks like a stolen session.
	Seed        string
	UserDataDir string // empty = launcher default (a fresh temp profile)
}

// NewSeed returns a fingerprint seed in CloakBrowser's range.
func NewSeed() string { return strconv.Itoa(10000 + rand.Intn(90000)) }

// launch starts the provisioned CloakBrowser Chromium with its stealth flags —
// fingerprint seed, spoofed platform, Vietnam locale/timezone — minus the two
// Chromium defaults that leak automation, and connects to it. The returned
// cleanup closes the browser and removes its temp profile.
func launch(ctx context.Context, o launchOpts) (*rod.Browser, func(), error) {
	bin, err := EnsureBinary(ctx)
	if err != nil {
		return nil, nil, err
	}
	fpPlatform := "windows"
	if runtime.GOOS == "darwin" {
		fpPlatform = "macos"
	}
	seed := o.Seed
	if seed == "" {
		seed = NewSeed()
	}

	l := launcher.New().
		Bin(bin).
		Context(ctx).
		Headless(o.Headless).
		Set(flags.Flag("no-sandbox")).
		Set(flags.Flag("fingerprint"), seed).
		Set(flags.Flag("fingerprint-platform"), fpPlatform).
		Set(flags.Flag("lang"), "vi-VN").
		Set(flags.Flag("fingerprint-timezone"), "Asia/Ho_Chi_Minh").
		Delete(flags.Flag("enable-automation")).
		Delete(flags.Flag("enable-unsafe-swiftshader"))
	if o.UserDataDir != "" {
		l = l.UserDataDir(o.UserDataDir)
	}

	controlURL, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("browser: launch: %w%s", err, LibsHint())
	}
	b := rod.New().ControlURL(controlURL).Context(ctx)
	if err := b.Connect(); err != nil {
		l.Cleanup()
		return nil, nil, fmt.Errorf("browser: connect: %w", err)
	}
	return b, func() { _ = b.Close(); l.Cleanup() }, nil
}

// LibsHint explains the most common launch failure on minimal Linux (WSL,
// containers): Chromium's NSS/ALSA libraries aren't installed. "" when fine.
func LibsHint() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := os.Stat("/usr/lib/x86_64-linux-gnu/libnss3.so"); err == nil {
		return ""
	}
	if _, err := os.Stat("/usr/lib/aarch64-linux-gnu/libnss3.so"); err == nil {
		return ""
	}
	return " — Chromium needs system libraries; run: sudo apt install -y libnss3 libasound2t64"
}
