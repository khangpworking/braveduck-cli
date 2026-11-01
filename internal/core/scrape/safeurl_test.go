package scrape

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestCheckPublicURL(t *testing.T) {
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })
	lookupIP = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		switch host {
		case "www.topcv.vn":
			return []netip.Addr{netip.MustParseAddr("104.18.10.10")}, nil
		case "rebind.example":
			return []netip.Addr{netip.MustParseAddr("104.18.10.10"), netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
	}

	ctx := context.Background()
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://www.topcv.vn/viec-lam/x", true},
		{"http://104.18.10.10/job", true},
		{"http://127.0.0.1:8080/api/jobs", false},
		{"http://localhost:6379/", false},
		{"http://[::1]/", false},
		{"http://192.168.1.1/admin", false},
		{"http://169.254.169.254/latest/meta-data", false},
		{"http://100.64.0.1/", false},
		{"http://0.0.0.0/", false},
		{"http://[::ffff:127.0.0.1]/", false},
		{"https://rebind.example/", false}, // any private answer taints the host
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"https:///nohost", false},
	} {
		err := CheckPublicURL(ctx, tc.url)
		if (err == nil) != tc.ok {
			t.Errorf("CheckPublicURL(%q) = %v, want ok=%v", tc.url, err, tc.ok)
		}
	}
}

// The dial-time guard is what stops redirects and rebinding, so check it on its
// own: a request straight at a loopback test server must be refused.
func TestPublicClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	_, err := publicClient.Get(srv.URL)
	if err == nil || !errors.Is(err, ErrNonPublicURL) {
		t.Fatalf("publicClient.Get(loopback) = %v, want ErrNonPublicURL", err)
	}
}
