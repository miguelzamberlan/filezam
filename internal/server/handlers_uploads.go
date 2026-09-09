package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zamberlan/filezam/internal/uploads"
	"github.com/zamberlan/filezam/internal/vfs"
)

func (s *Server) handleUploadCreate(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	var in struct {
		Dir       string `json:"dir"`
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		Mtime     *int64 `json:"mtime"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	dir, err := vfs.NormalizeWritable(in.Dir)
	if err != nil {
		return err
	}
	info, err := s.uploads.Create(r.Context(), root, u.Scope, u.ID, dir, in.Name, in.Size, in.Mtime, in.Overwrite)
	if err != nil {
		return err
	}
	writeJSON(w, r, 201, info)
	return nil
}

func (s *Server) handleUploadList(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	list, err := s.uploads.List(r.Context(), u.ID, u.Scope)
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"uploads": list})
	return nil
}

func (s *Server) handleUploadGet(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	info, err := s.uploads.Get(r.Context(), u.ID, u.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, info)
	return nil
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := s.acquireUpload(u)
	if err != nil {
		return err
	}
	defer release()
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 0 {
		return errorf(http.StatusBadRequest, "bad_index", "index query parameter required")
	}
	if r.ContentLength < 0 {
		return errorf(http.StatusLengthRequired, "length_required", "Content-Length required for chunks")
	}
	if r.ContentLength > s.cfg.ChunkSize {
		return errorf(http.StatusRequestEntityTooLarge, "too_large", "chunk exceeds chunk size")
	}
	info, err := s.uploads.WriteChunk(r.Context(), root, u.Scope, u.ID, r.PathValue("id"), index, r.ContentLength, bodyReader(w, r, s.cfg.ChunkSize))
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, info)
	return nil
}

func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	e, missing, err := s.uploads.Complete(r.Context(), root, u.Scope, u.ID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, uploads.ErrIncomplete) {
			ae := errorf(http.StatusConflict, "incomplete", "upload incomplete")
			ae.Extra = map[string]any{"missing": missing}
			return ae
		}
		return err
	}
	writeJSON(w, r, 200, map[string]any{"entry": e})
	return nil
}

func (s *Server) handleUploadAbort(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := s.uploads.Abort(r.Context(), root, u.Scope, u.ID, r.PathValue("id")); err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
