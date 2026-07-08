package browser

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

// Fetch renders u in the provisioned CloakBrowser Chromium and returns the fully
// hydrated HTML, so a JS-gated listing exposes the same schema.org/JobPosting
// ld+json a plain fetch would miss. It launches headless with CloakBrowser's
// stealth flags — a randomized fingerprint seed, a spoofed platform, and a Vietnam
// locale/timezone — and drops the two Chromium defaults that leak automation
// (navigator.webdriver and the SwiftShader software-WebGL renderer string).
func Fetch(ctx context.Context, u string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	bin, err := EnsureBinary(ctx)
	if err != nil {
		return "", err
	}

	fpPlatform := "windows"
	if runtime.GOOS == "darwin" {
		fpPlatform = "macos"
	}
	seed := strconv.Itoa(10000 + rand.Intn(90000))

	l := launcher.New().
		Bin(bin).
		Context(ctx).
		Headless(true).
		Set(flags.Flag("no-sandbox")).
		Set(flags.Flag("fingerprint"), seed).
		Set(flags.Flag("fingerprint-platform"), fpPlatform).
		Set(flags.Flag("lang"), "vi-VN").
		Set(flags.Flag("fingerprint-timezone"), "Asia/Ho_Chi_Minh").
		Delete(flags.Flag("enable-automation")).
		Delete(flags.Flag("enable-unsafe-swiftshader"))

	controlURL, err := l.Launch()
	if err != nil {
		return "", fmt.Errorf("cloakbrowser: launch: %w", err)
	}
	defer l.Cleanup()

	browser := rod.New().ControlURL(controlURL).Context(ctx)
	if err := browser.Connect(); err != nil {
		return "", fmt.Errorf("cloakbrowser: connect: %w", err)
	}
	defer browser.Close()

	// The board only needs the DOM and its ld+json — not pixels. Block images,
	// media, fonts, and stylesheets so a listing renders in a fraction of the
	// memory and bandwidth (this box is RAM-tight); loading them would only
	// inflate the footprint with bytes we never read.
	router := browser.HijackRequests()
	router.MustAdd("*", func(h *rod.Hijack) {
		switch h.Request.Type() {
		case proto.NetworkResourceTypeImage,
			proto.NetworkResourceTypeMedia,
			proto.NetworkResourceTypeFont,
			proto.NetworkResourceTypeStylesheet:
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
		default:
			h.ContinueRequest(&proto.FetchContinueRequest{})
		}
	})
	go router.Run()
	defer router.MustStop()

	page, err := browser.Page(proto.TargetCreateTarget{URL: u})
	if err != nil {
		return "", fmt.Errorf("cloakbrowser: open page: %w", err)
	}
	page = page.Context(ctx)

	// Best-effort: wait for load, then for the JobPosting block if the site emits
	// one. Missing either just means we return whatever HTML is present. The
	// timeout is scoped to a derived page so CancelTimeout releases that timer
	// without touching the parent context we read HTML from.
	page.WaitLoad()
	tp := page.Timeout(15 * time.Second)
	tp.Element(`script[type="application/ld+json"]`)
	tp.CancelTimeout()

	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("cloakbrowser: read html: %w", err)
	}
	return html, nil
}
