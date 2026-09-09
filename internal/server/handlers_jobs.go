package server

import "net/http"

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, 200, map[string]any{"jobs": s.jobs.List(userFrom(r).ID)})
	return nil
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) error {
	j, ok := s.jobs.Get(r.PathValue("id"), userFrom(r).ID)
	if !ok {
		return errorf(http.StatusNotFound, "not_found", "job not found")
	}
	writeJSON(w, r, 200, map[string]any{"job": j.Snapshot()})
	return nil
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) error {
	if !s.jobs.Cancel(r.PathValue("id"), userFrom(r).ID) {
		return errorf(http.StatusNotFound, "not_found", "job not found")
	}
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
