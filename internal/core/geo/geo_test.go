package geo

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDistanceKM(t *testing.T) {
	benThanh := Point{10.7725, 106.6980}
	landmark81 := Point{10.7949, 106.7219} // ~3.6 km away
	hanoi := Point{21.0285, 105.8542}      // ~1140 km away
	if d := DistanceKM(benThanh, landmark81); math.Abs(d-3.6) > 0.5 {
		t.Errorf("Ben Thanh → Landmark 81 = %.2f km", d)
	}
	if d := DistanceKM(benThanh, hanoi); math.Abs(d-1140) > 20 {
		t.Errorf("HCMC → Hanoi = %.0f km", d)
	}
	if (Point{0, 0}).Valid() || (Point{91, 0}).Valid() || !benThanh.Valid() {
		t.Error("Valid() wrong")
	}
}

func TestNominatimPoliteAndParsed(t *testing.T) {
	var ua, q string
	var times []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, q = r.Header.Get("User-Agent"), r.URL.Query().Get("q")
		times = append(times, time.Now())
		if q == "nowhere" {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`[{"lat":"10.7725","lon":"106.698","display_name":"Chợ Bến Thành, Quận 1"},{"lat":"0","lon":"0","display_name":"bogus"}]`))
	}))
	defer srv.Close()
	n := NewNominatim("test")
	n.Endpoint, n.Interval = srv.URL, 200*time.Millisecond

	got, err := n.Search(context.Background(), "chợ bến thành", 5, Point{})
	if err != nil || len(got) != 1 || got[0].Label != "Chợ Bến Thành, Quận 1" {
		t.Fatalf("search = %+v, %v", got, err)
	}
	if !strings.HasPrefix(ua, "braveduck-cli/") || q != "chợ bến thành" {
		t.Fatalf("ua %q q %q", ua, q)
	}
	if _, err := n.Search(context.Background(), "nowhere", 5, Point{10.7, 106.7}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no match = %v", err)
	}
	if gap := times[1].Sub(times[0]); gap < 190*time.Millisecond {
		t.Fatalf("requests %v apart; the usage policy needs a gap", gap)
	}
}
