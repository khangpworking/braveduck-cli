package api

import (
	"net/http"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The profile as a form (service/profileform.go): the board's friendly
// editor. The Markdown stays the profile; the form edits its known lines.

type profileFormOut struct {
	Form    service.ProfileForm        `json:"form"`
	Options service.ProfileFormOptions `json:"options"`
}

func (s *server) getProfileForm(w http.ResponseWriter, r *http.Request) {
	f, opts, err := s.svc.ProfileFormNow(r.Context())
	reply(w, http.StatusOK, profileFormOut{f, opts}, err)
}

func (s *server) putProfileForm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Form service.ProfileForm `json:"form"`
	}
	if !decode(w, r, &in) {
		return
	}
	f, err := s.svc.SaveProfileForm(r.Context(), in.Form)
	reply(w, http.StatusOK, profileFormOut{f, service.FormOptions}, err)
}
