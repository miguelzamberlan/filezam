package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/uploads"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// apiError carries an HTTP status and a stable machine-readable code.
type apiError struct {
	Status  int
	Code    string
	Message string
	Extra   map[string]any
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func errorf(status int, code, format string, args ...any) *apiError {
	return &apiError{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

var (
	errUnauthorized = errorf(http.StatusUnauthorized, "unauthorized", "authentication required")
	errForbidden    = errorf(http.StatusForbidden, "forbidden", "not allowed")
	errCSRF         = errorf(http.StatusForbidden, "csrf", "cross-site request rejected")
	errBadJSON      = errorf(http.StatusBadRequest, "bad_json", "invalid JSON body")
	errShareLocked  = errorf(http.StatusUnauthorized, "share_locked", "this link requires a password")
	errTOTPRequired = errorf(http.StatusForbidden, "totp_required", "two-factor authentication must be set up first")
)

// toAPIError maps domain errors to API errors.
func toAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	switch {
	case errors.Is(err, vfs.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return errorf(http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, vfs.ErrExists):
		return errorf(http.StatusConflict, "exists", "%s", err.Error())
	case errors.Is(err, vfs.ErrIsDir):
		return errorf(http.StatusConflict, "is_dir", "target is a directory")
	case errors.Is(err, vfs.ErrNotDir):
		return errorf(http.StatusConflict, "not_dir", "not a directory")
	case errors.Is(err, vfs.ErrNoSpace):
		return errorf(http.StatusInsufficientStorage, "no_space", "no space left on device")
	case errors.Is(err, vfs.ErrInvalidPath), errors.Is(err, vfs.ErrEscape), errors.Is(err, vfs.ErrTooManyLinks):
		return errorf(http.StatusBadRequest, "invalid_path", "invalid path")
	case errors.Is(err, vfs.ErrInvalidName):
		return errorf(http.StatusBadRequest, "invalid_name", "%s", err.Error())
	case errors.Is(err, vfs.ErrNested):
		return errorf(http.StatusBadRequest, "nested", "destination is inside source")
	case errors.Is(err, vfs.ErrRootOp):
		return errorf(http.StatusBadRequest, "root_op", "operation not allowed on the root folder")
	case errors.Is(err, vfs.ErrPermission):
		return errorf(http.StatusForbidden, "fs_permission", "filesystem permission denied")
	case errors.Is(err, vfs.ErrCrossDevice):
		return errorf(http.StatusConflict, "cross_device", "cannot move across devices")
	case errors.Is(err, uploads.ErrReserveExceeded):
		return errorf(http.StatusRequestEntityTooLarge, "upload_reserve_exceeded", "unfinished uploads already reserve too much space; finish or cancel them")
	case errors.Is(err, uploads.ErrInProgress):
		return errorf(http.StatusConflict, "upload_in_progress", "an upload for this target is already in progress")
	case errors.Is(err, uploads.ErrBadLength):
		return errorf(http.StatusBadRequest, "bad_length", "%s", err.Error())
	case errors.Is(err, uploads.ErrBadIndex):
		return errorf(http.StatusBadRequest, "bad_index", "chunk index out of range")
	case errors.Is(err, store.ErrConflict):
		return errorf(http.StatusConflict, "conflict", "already exists")
	case errors.Is(err, context.Canceled):
		return errorf(499, "cancelled", "request cancelled")
	case errors.Is(err, http.ErrNotSupported):
		return errorf(http.StatusBadRequest, "unsupported", "%s", err.Error())
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return errorf(http.StatusRequestEntityTooLarge, "too_large", "request body too large")
	}
	return &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error"}
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":{"code":"internal","message":"encode"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if len(buf) > 1400 && r != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		var zb bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&zb, gzip.BestSpeed)
		zw.Write(buf)
		zw.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.WriteHeader(status)
		w.Write(zb.Bytes())
		return
	}
	w.WriteHeader(status)
	w.Write(buf)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	ae := toAPIError(err)
	body := map[string]any{"code": ae.Code, "message": ae.Message}
	for k, v := range ae.Extra {
		body[k] = v
	}
	if ae.Status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "1")
	}
	writeJSON(w, r, ae.Status, map[string]any{"error": body})
}

const maxJSONBody = 1 << 20

func readJSON(r *http.Request, v any) error {
	body := http.MaxBytesReader(nil, r.Body, maxJSONBody)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errorf(http.StatusRequestEntityTooLarge, "too_large", "JSON body too large")
		}
		return errBadJSON
	}
	if dec.More() {
		return errBadJSON
	}
	io.Copy(io.Discard, body)
	return nil
}

// handlerFunc is an http handler that returns an error.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (s *Server) h(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			ae := toAPIError(err)
			if ae.Status >= 500 {
				s.log.Error("request failed", "method", r.Method, "path", logPath(r.URL.Path), "err", err)
			}
			writeError(w, r, err)
		}
	})
}
