package api

import (
	"net/http"

	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The profile as a form (service/profileform.go): the board's friendly
// editor. The Markdown stays the profile; the form edits its known lines.

func (s *server) getProfileForm(w http.ResponseWriter, r *http.Request) {
	f, opts, err := s.svc.ProfileFormNow(r.Context())
	reply(w, http.StatusOK, ProfileFormReply{f, opts}, err)
}

func (s *server) putProfileForm(w http.ResponseWriter, r *http.Request) {
	var in ProfileFormRequest
	if !decode(w, r, &in) {
		return
	}
	f, err := s.svc.SaveProfileForm(r.Context(), in.Form)
	reply(w, http.StatusOK, ProfileFormReply{f, service.FormOptions}, err)
}
