package api

import (
	"context"
	"net/http"

	"github.com/khangpworking/braveduck-cli/internal/core/geo"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

func (s *server) getLocation(w http.ResponseWriter, r *http.Request) {
	l, err := s.svc.GetLocation(r.Context())
	reply(w, http.StatusOK, l, err)
}

// putLocation sets the user's location (from the browser's location prompt,
// a map click or a chosen search result) and resolves job addresses in the
// background.
func (s *server) putLocation(w http.ResponseWriter, r *http.Request) {
	var in LocationRequest
	if !decode(w, r, &in) {
		return
	}
	l, err := s.svc.SetLocation(r.Context(), geo.Point{Lat: in.Lat, Lng: in.Lng}, in.Label, in.MaxKM)
	if err == nil {
		go s.svc.UpdateDistances(context.Background(), service.MaxLookupsPerPass)
	}
	reply(w, http.StatusOK, l, err)
}

func (s *server) deleteLocation(w http.ResponseWriter, r *http.Request) {
	err := s.svc.ClearLocation(r.Context())
	reply(w, http.StatusOK, map[string]bool{"cleared": true}, err)
}

// searchPlaces geocodes a typed address (OpenStreetMap Nominatim).
func (s *server) searchPlaces(w http.ResponseWriter, r *http.Request) {
	places, err := s.svc.SearchPlaces(r.Context(), r.URL.Query().Get("q"))
	reply(w, http.StatusOK, places, err)
}
