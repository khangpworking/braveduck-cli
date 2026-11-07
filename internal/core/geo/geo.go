// Package geo turns addresses into coordinates and measures distance, for
// "how far is this job from me". Geocoding uses OpenStreetMap's Nominatim
// (free, no key) and follows its usage policy: at most one request per
// second, an identifying User-Agent, and callers cache results so each
// address is looked up once. https://operations.osmfoundation.org/policies/nominatim/
package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Valid reports whether p is a real coordinate (0,0 means "unknown" in the
// sources braveduck reads).
func (p Point) Valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180 && !(p.Lat == 0 && p.Lng == 0)
}

// DistanceKM is the great-circle (haversine) distance between a and b.
func DistanceKM(a, b Point) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLng := (b.Lat-a.Lat)*rad, (b.Lng-a.Lng)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

// Place is one geocoding result.
type Place struct {
	Point `tstype:",extends"`
	Label string `json:"label"`
}

// ErrNotFound means the address matched nothing.
var ErrNotFound = errors.New("address not found")

// Nominatim is a polite client for nominatim.openstreetmap.org.
type Nominatim struct {
	Endpoint  string // overridable in tests
	UserAgent string
	Client    *http.Client
	Interval  time.Duration // minimum gap between requests

	mu   sync.Mutex
	last time.Time
}

// NewNominatim returns a client for the public OpenStreetMap instance.
func NewNominatim(version string) *Nominatim {
	return &Nominatim{
		Endpoint:  "https://nominatim.openstreetmap.org/search",
		UserAgent: "braveduck-cli/" + version + " (local job board; github.com/khangpworking/braveduck-cli)",
		Client:    &http.Client{Timeout: 20 * time.Second},
		Interval:  1100 * time.Millisecond,
	}
}

// Search returns up to limit places for query (a free-text address). A valid
// near point biases results toward it (no country is assumed: "Quận 1"
// finds the one near the user).
func (n *Nominatim) Search(ctx context.Context, query string, limit int, near Point) ([]Place, error) {
	if limit < 1 || limit > 10 {
		limit = 5
	}
	if err := n.wait(ctx); err != nil {
		return nil, err
	}
	q := url.Values{"q": {query}, "format": {"jsonv2"}, "limit": {strconv.Itoa(limit)}, "accept-language": {"vi,en"}}
	if near.Valid() {
		const d = 1.5 // degrees, ~150 km: a preference, not a filter (bounded=0)
		q.Set("viewbox", fmt.Sprintf("%f,%f,%f,%f", near.Lng-d, near.Lat+d, near.Lng+d, near.Lat-d))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.Endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", n.UserAgent)
	resp, err := n.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocoding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding: HTTP %d", resp.StatusCode)
	}
	var raw []struct {
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("geocoding: unexpected response: %w", err)
	}
	var out []Place
	for _, r := range raw {
		lat, e1 := strconv.ParseFloat(r.Lat, 64)
		lng, e2 := strconv.ParseFloat(r.Lon, 64)
		if e1 == nil && e2 == nil {
			if p := (Point{lat, lng}); p.Valid() {
				out = append(out, Place{Point: p, Label: r.DisplayName})
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// wait enforces the minimum gap between requests across goroutines.
func (n *Nominatim) wait(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if d := n.Interval - time.Since(n.last); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	n.last = time.Now()
	return nil
}
