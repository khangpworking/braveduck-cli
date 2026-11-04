package api

import "net/http"

// The profile editor and the "Get started" checklist.

func (s *server) getProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.svc.Profile(r.Context())
	reply(w, http.StatusOK, p, err)
}

func (s *server) putProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := s.svc.SaveProfile(r.Context(), req.Text)
	reply(w, http.StatusOK, p, err)
}

func (s *server) getSetup(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Setup(r.Context())
	reply(w, http.StatusOK, st, err)
}
