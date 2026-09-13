// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/uploads"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// sessionRef points at one of the logged-in user's own sessions.
func (s *Server) sessionRef(r *http.Request, u *store.User) uploads.SessionRef {
	return uploads.SessionRef{ID: r.PathValue("id"), Scope: u.Scope, UserID: u.ID}
}

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
	if err := s.checkQuota(r.Context(), u, in.Size); err != nil {
		return err
	}
	info, err := s.uploads.Create(r.Context(), root, uploads.CreateOpts{Scope: u.Scope, Dir: dir, Name: in.Name, UserID: u.ID, Size: in.Size, Mtime: in.Mtime, Overwrite: in.Overwrite})
	if err == nil {
		s.usageAdd(u.ID, in.Size)
	}
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
	info, err := s.uploads.Get(r.Context(), s.sessionRef(r, u))
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
	info, err := s.uploads.WriteChunk(r.Context(), root, s.sessionRef(r, u), index, r.ContentLength, bodyReader(w, r, s.cfg.ChunkSize))
	if err == nil && r.ContentLength > 0 {
		s.metrics.Inc("filezam_upload_bytes_total", "", r.ContentLength)
	}
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
	info, _ := s.uploads.Get(r.Context(), s.sessionRef(r, u))
	e, missing, err := s.uploads.Complete(r.Context(), root, s.sessionRef(r, u))
	if err != nil {
		if errors.Is(err, uploads.ErrIncomplete) {
			ae := errorf(http.StatusConflict, "incomplete", "upload incomplete")
			ae.Extra = map[string]any{"missing": missing}
			return ae
		}
		return err
	}
	if info != nil {
		s.indexTouch(indexAncestors(u.Scope, vfs.Join(u.Scope, info.Dir, info.Name))...)
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
	if err := s.uploads.Abort(r.Context(), root, s.sessionRef(r, u)); err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
