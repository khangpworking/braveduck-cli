package api

import "net/http"

// Followed Facebook groups (service/facebook.go). Following one is pasting
// its link in + Add Job (createJob).

func (s *server) listFBGroups(w http.ResponseWriter, r *http.Request) {
	gs, err := s.svc.FBGroups(r.Context())
	reply(w, http.StatusOK, gs, err)
}

func (s *server) unfollowFBGroup(w http.ResponseWriter, r *http.Request) {
	err := s.svc.UnfollowGroup(r.Context(), r.PathValue("key"))
	reply(w, http.StatusOK, map[string]bool{"unfollowed": true}, err)
}
