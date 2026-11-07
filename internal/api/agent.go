package api

import "net/http"

// The AI agent's key on a shared server (service/agent.go): the board makes
// one and shows it once, or removes it.

func (s *server) getAgentKey(w http.ResponseWriter, r *http.Request) {
	ok, err := s.svc.HasAgentKey(r.Context())
	reply(w, http.StatusOK, AgentKeyState{Exists: ok}, err)
}

func (s *server) newAgentKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.svc.NewAgentKey(r.Context())
	reply(w, http.StatusCreated, NewAgentKey{Key: key}, err)
}

func (s *server) revokeAgentKey(w http.ResponseWriter, r *http.Request) {
	err := s.svc.RevokeAgentKey(r.Context())
	reply(w, http.StatusOK, AgentKeyState{}, err)
}
