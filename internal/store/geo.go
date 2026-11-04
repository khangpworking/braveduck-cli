package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// UserLocation is where the user lives/works from, and how far they'll go.
type UserLocation struct {
	Lat, Lng float64
	Label    string
	MaxKM    float64
}

// GetUserLocation returns the user's location; ok is false if unset.
func (s *Store) GetUserLocation(ctx context.Context) (UserLocation, bool, error) {
	var l UserLocation
	err := s.db.QueryRowContext(ctx, "SELECT lat, lng, label, max_km FROM user_location WHERE id = 1").Scan(&l.Lat, &l.Lng, &l.Label, &l.MaxKM)
	if errors.Is(err, sql.ErrNoRows) {
		return UserLocation{}, false, nil
	}
	return l, err == nil, err
}

// SetUserLocation stores the user's location.
func (s *Store) SetUserLocation(ctx context.Context, l UserLocation) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_location (id, lat, lng, label, max_km) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET lat = excluded.lat, lng = excluded.lng, label = excluded.label, max_km = excluded.max_km`,
		l.Lat, l.Lng, l.Label, l.MaxKM)
	return err
}

// ClearUserLocation forgets the user's location and every job distance.
func (s *Store) ClearUserLocation(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM user_location"); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET company_distance_km = NULL, company_geo_status = NULL")
	return err
}

// Geocoded is a cached lookup; Found false means the address matched nothing.
type Geocoded struct {
	Found    bool
	Lat, Lng float64
	Label    string
}

// GetGeocode returns the cached lookup for query; ok is false if never looked up.
func (s *Store) GetGeocode(ctx context.Context, query string) (Geocoded, bool, error) {
	var g Geocoded
	var found int
	var lat, lng sql.NullFloat64
	var label sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT found, lat, lng, label FROM geocode_cache WHERE query = ?", query).Scan(&found, &lat, &lng, &label)
	if errors.Is(err, sql.ErrNoRows) {
		return Geocoded{}, false, nil
	}
	if err != nil {
		return Geocoded{}, false, err
	}
	g.Found, g.Lat, g.Lng, g.Label = found == 1, lat.Float64, lng.Float64, label.String
	return g, true, nil
}

// PutGeocode caches a lookup (including "not found", so it isn't retried).
func (s *Store) PutGeocode(ctx context.Context, query string, g Geocoded, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO geocode_cache (query, found, lat, lng, label, at) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(query) DO UPDATE SET found = excluded.found, lat = excluded.lat, lng = excluded.lng, label = excluded.label, at = excluded.at`,
		query, b2i(g.Found), g.Lat, g.Lng, g.Label, now.UTC().Format(time.RFC3339))
	return err
}

// SetJobDistance records a job's distance from the user and the geo status
// ("ok" with a distance; "no_address" / "geocode_failed" without).
func (s *Store) SetJobDistance(ctx context.Context, id int64, km float64, hasKM bool, status string) error {
	var d any
	if hasKM {
		d = km
	}
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET company_distance_km = ?, company_geo_status = ? WHERE id = ?", d, status, id)
	return err
}
