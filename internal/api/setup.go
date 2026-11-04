package api

import "net/http"

// The profile editor, the "Get started" checklist and Today.

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

func (s *server) getToday(w http.ResponseWriter, r *http.Request) {
	g, err := s.svc.Today(r.Context())
	reply(w, http.StatusOK, g, err)
}
