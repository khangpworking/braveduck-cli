//go:build live

package company

import (
	"context"
	"errors"
	"testing"
)

// Checks the parser against the real site (a handful of requests, 2 s
// apart). Not run in CI; run it when the site may have changed:
//
//	go test -tags live -v ./internal/core/company/
func TestLiveRegister(t *testing.T) {
	reg := New()
	rec, err := reg.Find(context.Background(), "Cty TNHH TM SX Thiên Ấn")
	if err != nil || rec.TaxCode != "0309542960" || rec.Founded != "2009-10-20" || rec.Status == "" {
		t.Fatalf("Thiên Ấn: %+v, %v", rec, err)
	}
	t.Logf("found: %+v", rec)
	if _, err := reg.Find(context.Background(), "FPT Software"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a brand name: %v, want ErrNotFound", err)
	}
}
