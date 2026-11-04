package browser

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// RenderPDF prints an HTML document to an A4 PDF with the built-in browser.
// The page is loaded from memory and network access is cut off, so a
// document can't fetch anything.
func RenderPDF(ctx context.Context, html string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	b, cleanup, err := launch(ctx, launchOpts{Headless: true})
	if err != nil {
		return nil, err
	}
	defer cleanup()
	pg, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, err
	}
	pg = pg.Context(ctx)
	if err := (proto.NetworkEmulateNetworkConditions{Offline: true, Latency: 0, DownloadThroughput: -1, UploadThroughput: -1}).Call(pg); err != nil {
		return nil, fmt.Errorf("pdf: offline mode: %w", err)
	}
	if err := pg.SetDocumentContent(html); err != nil {
		return nil, err
	}
	r, err := pg.PDF(&proto.PagePrintToPDF{PrintBackground: true, PreferCSSPageSize: true})
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	return io.ReadAll(r)
}
