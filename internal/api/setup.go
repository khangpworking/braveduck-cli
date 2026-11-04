package api

import "net/http"

// The profile editor, the "Get started" checklist, Today and the export.

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

// exportCSV downloads every job as a spreadsheet.
func (s *server) exportCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="braveduck-jobs.csv"`)
	if err := s.svc.ExportCSV(r.Context(), w); err != nil {
		reply(w, 0, nil, err)
	}
}
