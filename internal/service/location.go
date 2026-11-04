package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/khangpworking/braveduck-cli/internal/core/geo"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

// The user's location: set from the browser's own location prompt (no
// external lookup), a click on the map, or a typed address (geocoded with
// OpenStreetMap Nominatim). Job distances follow from it. The location never
// leaves the machine except as the bias of an address search.

// Location is the user's location as the faces see it.
type Location struct {
	Set   bool    `json:"set"`
	Lat   float64 `json:"lat,omitempty"`
	Lng   float64 `json:"lng,omitempty"`
	Label string  `json:"label,omitempty"`
	MaxKM float64 `json:"max_km"` // the user's maximum commute; jobs beyond it are flagged
}

// DefaultMaxKM is the maximum commute until the user sets one.
const DefaultMaxKM = 10

// geocoder is the address lookup; swapped in tests.
type geocoder interface {
	Search(ctx context.Context, query string, limit int, near geo.Point) ([]geo.Place, error)
}

var (
	geoOnce sync.Once
	geoImpl geocoder
)

// SetGeocoder replaces the address lookup (tests).
func (s *Service) SetGeocoder(g geocoder) { s.geo = g }

func (s *Service) geocoder() geocoder {
	if s.geo != nil {
		return s.geo
	}
	geoOnce.Do(func() { geoImpl = geo.NewNominatim("dev") })
	return geoImpl
}

// GetLocation returns the user's location (Set false when unset).
func (s *Service) GetLocation(ctx context.Context) (Location, error) {
	l, ok, err := s.st.GetUserLocation(ctx)
	if err != nil || !ok {
		return Location{MaxKM: DefaultMaxKM}, err
	}
	return Location{Set: true, Lat: l.Lat, Lng: l.Lng, Label: l.Label, MaxKM: l.MaxKM}, nil
}

// SetLocation stores the user's location and maximum commute (0 keeps the
// current one, or the default), then recomputes every job's distance from
// cached geocodes (new lookups happen in UpdateDistances).
func (s *Service) SetLocation(ctx context.Context, p geo.Point, label string, maxKM float64) (Location, error) {
	if !p.Valid() {
		return Location{}, fmt.Errorf("%w: %v, %v isn't a real coordinate", ErrInvalid, p.Lat, p.Lng)
	}
	if maxKM < 0 || maxKM > 500 {
		return Location{}, fmt.Errorf("%w: maximum commute must be 1-500 km", ErrInvalid)
	}
	if maxKM == 0 {
		cur, _ := s.GetLocation(ctx)
		maxKM = cur.MaxKM
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > 200 {
		label = string([]rune(label)[:200])
	}
	if err := s.st.SetUserLocation(ctx, store.UserLocation{Lat: p.Lat, Lng: p.Lng, Label: label, MaxKM: maxKM}); err != nil {
		return Location{}, err
	}
	s.logEvent(ctx, "info", "Location updated")
	_, _ = s.UpdateDistances(ctx, 0) // cached only; new addresses are looked up in the background
	return s.GetLocation(ctx)
}

// ClearLocation forgets the user's location and all job distances.
func (s *Service) ClearLocation(ctx context.Context) error {
	return s.st.ClearUserLocation(ctx)
}

// SearchPlaces geocodes an address the user typed, biased toward their
// current location.
func (s *Service) SearchPlaces(ctx context.Context, query string) ([]geo.Place, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 3 {
		return nil, fmt.Errorf("%w: type at least 3 characters", ErrInvalid)
	}
	cur, _ := s.GetLocation(ctx)
	places, err := s.geocoder().Search(ctx, query, 5, geo.Point{Lat: cur.Lat, Lng: cur.Lng})
	if errors.Is(err, geo.ErrNotFound) {
		return []geo.Place{}, nil
	}
	return places, err
}

// DistanceResult is what one UpdateDistances pass did.
type DistanceResult struct {
	Measured int `json:"measured"` // jobs with a distance now
	Lookups  int `json:"lookups"`  // new addresses sent to the geocoder
	Pending  int `json:"pending"`  // addresses left for a later pass (lookup cap)
}

// MaxLookupsPerPass keeps address lookups light (OpenStreetMap policy).
const MaxLookupsPerPass = 20

// UpdateDistances sets every job's distance from the user's location: cached
// addresses at once, up to maxLookups new ones through the geocoder (0 = none).
// Jobs with no address are marked so the board can say so.
func (s *Service) UpdateDistances(ctx context.Context, maxLookups int) (DistanceResult, error) {
	var res DistanceResult
	me, err := s.GetLocation(ctx)
	if err != nil || !me.Set {
		return res, err
	}
	home := geo.Point{Lat: me.Lat, Lng: me.Lng}
	all, err := s.st.List(ctx, store.ListFilter{})
	if err != nil {
		return res, err
	}
	for _, j := range all {
		addr := strings.TrimSpace(j.Location)
		if addr == "" {
			if j.CompanyGeoStatus != "no_address" {
				_ = s.st.SetJobDistance(ctx, j.ID, 0, false, "no_address")
			}
			continue
		}
		key := strings.ToLower(addr)
		g, cached, err := s.st.GetGeocode(ctx, key)
		if err != nil {
			return res, err
		}
		if !cached {
			if res.Lookups >= maxLookups {
				res.Pending++
				continue
			}
			res.Lookups++
			places, err := s.geocoder().Search(ctx, addr, 1, home)
			switch {
			case errors.Is(err, geo.ErrNotFound):
				g = store.Geocoded{Found: false}
			case err != nil:
				if ctx.Err() != nil {
					return res, ctx.Err()
				}
				continue // network trouble: try again next pass, don't cache
			default:
				g = store.Geocoded{Found: true, Lat: places[0].Lat, Lng: places[0].Lng, Label: places[0].Label}
			}
			_ = s.st.PutGeocode(ctx, key, g, s.now())
		}
		if !g.Found {
			_ = s.st.SetJobDistance(ctx, j.ID, 0, false, "geocode_failed")
			continue
		}
		km := geo.DistanceKM(home, geo.Point{Lat: g.Lat, Lng: g.Lng})
		_ = s.st.SetJobDistance(ctx, j.ID, km, true, "ok")
		res.Measured++
	}
	return res, nil
}
