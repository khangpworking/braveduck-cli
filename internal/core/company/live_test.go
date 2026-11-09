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
	rec, err := reg.Find(context.Background(), "Cty TNHH TM SX Thiên Ấn", "Ho Chi Minh")
	if err != nil || rec.TaxCode != "0309542960" || rec.Founded != "2009-10-20" || rec.Status == "" {
		t.Fatalf("Thiên Ấn: %+v, %v", rec, err)
	}
	t.Logf("found: %+v", rec)
	for _, tc := range []struct{ name, location, tax string }{
		{"CÔNG TY CỔ PHẦN VIỄN THÔNG FPT", "Hà Nội", "0101778163"},
		{"CÔNG TY CỔ PHẦN CÔNG NGHỆ PREP", "Hà Nội", "0109817671"},
		{"BOSCH GLOBAL SOFTWARE TECHNOLOGIES COMPANY LIMITED", "Etown Building, 364 Cong Hoa, Tan Binh, Ho Chi Minh", "0310409233"},
	} {
		if rec, err := reg.Find(context.Background(), tc.name, tc.location); err != nil || rec.TaxCode != tc.tax {
			t.Errorf("%s: %+v, %v", tc.name, rec, err)
		} else {
			t.Logf("found: %+v", rec)
		}
	}
	if _, err := reg.Find(context.Background(), "FPT Software", "Ha Noi"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a brand name: %v, want ErrNotFound", err)
	}
}
