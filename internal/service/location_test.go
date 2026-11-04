package service

import (
	"context"
	"errors"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/geo"
)

type fakeGeo struct {
	places map[string]geo.Point
	calls  int
}

func (f *fakeGeo) Search(_ context.Context, q string, _ int, _ geo.Point) ([]geo.Place, error) {
	f.calls++
	p, ok := f.places[q]
	if !ok {
		return nil, geo.ErrNotFound
	}
	return []geo.Place{{Point: p, Label: q}}, nil
}

func TestLocationAndDistances(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	fg := &fakeGeo{places: map[string]geo.Point{
		"Landmark 81, Bình Thạnh": {Lat: 10.7949, Lng: 106.7219},
		"Hà Nội":                  {Lat: 21.0285, Lng: 105.8542},
	}}
	s.SetGeocoder(fg)
	near, _ := s.AddJob(ctx, NewJob{Title: "Near"})
	s.UpdateJob(ctx, near.ID, Patch{Location: ptr("Landmark 81, Bình Thạnh")})
	far, _ := s.AddJob(ctx, NewJob{Title: "Far"})
	s.UpdateJob(ctx, far.ID, Patch{Location: ptr("Hà Nội")})
	unknown, _ := s.AddJob(ctx, NewJob{Title: "Unknown"})
	s.UpdateJob(ctx, unknown.ID, Patch{Location: ptr("Nowhere street 999")})
	noaddr, _ := s.AddJob(ctx, NewJob{Title: "No address"})

	if l, _ := s.GetLocation(ctx); l.Set || l.MaxKM != DefaultMaxKM {
		t.Fatalf("fresh location = %+v", l)
	}
	if _, err := s.SetLocation(ctx, geo.Point{}, "", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("0,0 accepted: %v", err)
	}
	l, err := s.SetLocation(ctx, geo.Point{Lat: 10.7725, Lng: 106.6980}, "Bến Thành", 8)
	if err != nil || !l.Set || l.MaxKM != 8 {
		t.Fatalf("set = %+v, %v", l, err)
	}
	if fg.calls != 0 {
		t.Fatal("setting the location sent addresses to the geocoder (only cached ones are used inline)")
	}

	res, err := s.UpdateDistances(ctx, 2)
	if err != nil || res.Lookups != 2 || res.Pending != 1 {
		t.Fatalf("first pass = %+v, %v (cap of 2 lookups)", res, err)
	}
	res, _ = s.UpdateDistances(ctx, 2)
	if res.Lookups != 1 || res.Pending != 0 {
		t.Fatalf("second pass = %+v", res)
	}
	calls := fg.calls
	s.UpdateDistances(ctx, 20)
	if fg.calls != calls {
		t.Fatal("cached addresses looked up again")
	}

	j, _ := s.GetJob(ctx, near.ID)
	if !j.HasDistance || j.CompanyDistanceKM < 3 || j.CompanyDistanceKM > 4.5 || j.CompanyGeoStatus != "ok" {
		t.Fatalf("near = %v km %q", j.CompanyDistanceKM, j.CompanyGeoStatus)
	}
	if j, _ := s.GetJob(ctx, far.ID); j.CompanyDistanceKM < 1000 {
		t.Fatalf("far = %v km", j.CompanyDistanceKM)
	}
	if j, _ := s.GetJob(ctx, unknown.ID); j.CompanyGeoStatus != "geocode_failed" || j.HasDistance {
		t.Fatalf("unknown = %q", j.CompanyGeoStatus)
	}
	if j, _ := s.GetJob(ctx, noaddr.ID); j.CompanyGeoStatus != "no_address" {
		t.Fatalf("no address = %q", j.CompanyGeoStatus)
	}

	// Moving recomputes from the cache, without new lookups.
	calls = fg.calls
	s.SetLocation(ctx, geo.Point{Lat: 21.03, Lng: 105.85}, "Hà Nội", 0)
	if j, _ := s.GetJob(ctx, far.ID); j.CompanyDistanceKM > 1 {
		t.Fatalf("after moving to Hanoi, Hanoi job is %v km", j.CompanyDistanceKM)
	}
	if fg.calls != calls {
		t.Fatal("moving triggered lookups")
	}
	if l, _ := s.GetLocation(ctx); l.MaxKM != 8 {
		t.Fatalf("max km not kept when 0 passed: %v", l.MaxKM)
	}

	s.ClearLocation(ctx)
	if j, _ := s.GetJob(ctx, near.ID); j.HasDistance {
		t.Fatal("distances kept after clearing the location")
	}
}

func TestSearchPlaces(t *testing.T) {
	s := newSvc(t)
	s.SetGeocoder(&fakeGeo{places: map[string]geo.Point{"Quận 1": {Lat: 10.77, Lng: 106.70}}})
	if _, err := s.SearchPlaces(context.Background(), "Q1"); !errors.Is(err, ErrInvalid) {
		t.Fatal("2-char search accepted")
	}
	if p, _ := s.SearchPlaces(context.Background(), "Quận 1"); len(p) != 1 {
		t.Fatalf("search = %+v", p)
	}
	if p, err := s.SearchPlaces(context.Background(), "zzzzzz"); err != nil || len(p) != 0 {
		t.Fatalf("no match = %+v, %v", p, err)
	}
}
