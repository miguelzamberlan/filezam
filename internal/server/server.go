// Package server implements the Filezam HTTP API and SPA hosting.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/config"
	"github.com/zamberlan/filezam/internal/jobs"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/uploads"
	"github.com/zamberlan/filezam/internal/vfs"
)

// Server holds all dependencies.
type Server struct {
	cfg     *config.Config
	db      *store.DB
	base    *vfs.Root
	uploads *uploads.Service
	jobs    *jobs.Manager
	log     *slog.Logger
	version string

	loginIP   *auth.Limiter
	loginUser *auth.Limiter
	loginSem  auth.Semaphore
	publicIP  *auth.Limiter
	publicDL  *auth.KeyedSemaphore
	uploadSem *auth.KeyedSemaphore
	searchSem *auth.KeyedSemaphore
	adminMu   sync.Mutex // serializa alterações de usuários: a checagem de "último admin" não é atômica no banco

	bg     context.Context
	cancel context.CancelFunc
}

// New wires the server. It ensures the first admin exists.
func New(cfg *config.Config, db *store.DB, base *vfs.Root, log *slog.Logger, version string) (*Server, error) {
	bg, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg: cfg, db: db, base: base, log: log, version: version,
		uploads:   uploads.New(db, cfg.ChunkSize, cfg.Fsync, log),
		jobs:      jobs.New(bg),
		loginIP:   auth.NewLimiter(10, 10),
		loginUser: auth.NewLimiter(5, 5),
		loginSem:  auth.NewSemaphore(4),
		publicIP:  auth.NewLimiter(120, 120),
		publicDL:  auth.NewKeyedSemaphore(2),
		uploadSem: auth.NewKeyedSemaphore(8),
		searchSem: auth.NewKeyedSemaphore(2),
		bg:        bg, cancel: cancel,
	}
	if err := s.ensureAdmin(bg); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func (s *Server) ensureAdmin(ctx context.Context) error {
	n, err := s.db.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := auth.HashPassword(s.cfg.AdminPassword)
	if err != nil {
		return err
	}
	_, err = s.db.CreateUser(ctx, &store.User{Username: s.cfg.AdminUser, PasswordHash: hash, Role: "admin", MustChangePassword: true})
	if err != nil {
		return fmt.Errorf("create initial admin: %w", err)
	}
	s.log.Warn("created initial admin user; password change is required at first login", "username", s.cfg.AdminUser)
	return nil
}

// CheckWritable verifies the data dir and root are writable by this process.
func CheckWritable(dirs ...string) error {
	for _, d := range dirs {
		probe := filepath.Join(d, ".filezam-write-probe")
		f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("directory %s is not writable by uid %d: %w (fix with: chown -R %d:%d %s)", d, os.Getuid(), err, os.Getuid(), os.Getgid(), d)
		}
		f.Close()
		os.Remove(probe)
	}
	return nil
}

// Handler builds the full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	return chain(mux, s.recoverer, s.realIP, s.securityHeaders, s.logging)
}

// StartBackground launches periodic maintenance.
func (s *Server) StartBackground() {
	run := func() {
		ctx, cancel := context.WithTimeout(s.bg, 5*time.Minute)
		defer cancel()
		s.uploads.CleanupStale(ctx, s.base, s.cfg.UploadStaleAge)
		if err := s.db.PurgeExpiredSessions(ctx); err != nil {
			s.log.Warn("purge sessions", "err", err)
		}
		if err := s.db.PruneAudit(ctx, time.Now().Add(-180*24*time.Hour).Unix()); err != nil {
			s.log.Warn("prune audit", "err", err)
		}
	}
	go func() {
		run()
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-s.bg.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

// Shutdown stops background work and waits for jobs.
func (s *Server) Shutdown(timeout time.Duration) {
	s.jobs.Wait(timeout)
	s.cancel()
}

// scopeRoot opens the user's scope root. Caller must Close it.
func (s *Server) scopeRoot(u *store.User) (*vfs.Root, error) {
	r, err := s.base.Sub(u.Scope)
	if err != nil {
		if errors.Is(err, vfs.ErrNotFound) || errors.Is(err, vfs.ErrNotDir) || errors.Is(err, vfs.ErrEscape) {
			return nil, errorf(http.StatusForbidden, "scope_unavailable", "your folder is not available; contact the administrator")
		}
		return nil, err
	}
	return r, nil
}

func (s *Server) routes(mux *http.ServeMux) {
	user := func(fn handlerFunc) http.Handler { return chain(s.h(fn), s.requireUser, s.requirePasswordOK) }
	admin := func(fn handlerFunc) http.Handler {
		return chain(s.h(fn), s.requireUser, s.requirePasswordOK, s.requireAdmin)
	}
	authed := func(fn handlerFunc) http.Handler { return chain(s.h(fn), s.requireUser) }

	mux.Handle("GET /api/health", s.h(s.handleHealth))
	mux.Handle("GET /api/config", authed(s.handleConfig))

	mux.Handle("POST /api/auth/login", chain(s.h(s.handleLogin), s.csrf))
	mux.Handle("POST /api/auth/logout", authed(s.handleLogout))
	mux.Handle("GET /api/auth/me", authed(s.handleMe))
	mux.Handle("POST /api/auth/password", authed(s.handleChangePassword))

	mux.Handle("GET /api/files", user(s.handleList))
	mux.Handle("GET /api/files/stat", user(s.handleStat))
	mux.Handle("GET /api/files/info", user(s.handleInfo))
	mux.Handle("GET /api/files/disk", user(s.handleDisk))
	mux.Handle("GET /api/files/search", user(s.handleSearch))
	mux.Handle("GET /api/files/content", user(s.handleContent))
	mux.Handle("PUT /api/files/content", user(s.handlePutContent))
	mux.Handle("POST /api/files/batch", user(s.handleBatch))
	mux.Handle("GET /api/files/zip", user(s.handleZip))
	mux.Handle("POST /api/files/mkdir", user(s.handleMkdir))
	mux.Handle("POST /api/files/rename", user(s.handleRename))
	mux.Handle("POST /api/files/delete", user(s.handleDelete))
	mux.Handle("POST /api/files/copy", user(s.handleCopy))
	mux.Handle("POST /api/files/move", user(s.handleMove))

	mux.Handle("GET /api/jobs", user(s.handleJobs))
	mux.Handle("GET /api/jobs/{id}", user(s.handleJob))
	mux.Handle("DELETE /api/jobs/{id}", user(s.handleJobCancel))

	mux.Handle("POST /api/uploads", user(s.handleUploadCreate))
	mux.Handle("GET /api/uploads", user(s.handleUploadList))
	mux.Handle("GET /api/uploads/{id}", user(s.handleUploadGet))
	mux.Handle("PUT /api/uploads/{id}", user(s.handleUploadChunk))
	mux.Handle("POST /api/uploads/{id}/complete", user(s.handleUploadComplete))
	mux.Handle("DELETE /api/uploads/{id}", user(s.handleUploadAbort))

	mux.Handle("GET /api/favorites", user(s.handleFavorites))
	mux.Handle("POST /api/favorites", user(s.handleFavoriteAdd))
	mux.Handle("DELETE /api/favorites/{id}", user(s.handleFavoriteDelete))

	mux.Handle("GET /api/shares", user(s.handleShares))
	mux.Handle("POST /api/shares", user(s.handleShareCreate))
	mux.Handle("DELETE /api/shares/{id}", user(s.handleShareDelete))

	mux.Handle("GET /api/public/{token}", s.h(s.handlePublicInfo))
	mux.Handle("GET /api/public/{token}/list", s.h(s.handlePublicList))
	mux.Handle("GET /api/public/{token}/content", s.h(s.handlePublicContent))
	mux.Handle("GET /api/public/{token}/zip", s.h(s.handlePublicZip))

	mux.Handle("GET /api/admin/users", admin(s.handleAdminUsers))
	mux.Handle("POST /api/admin/users", admin(s.handleAdminUserCreate))
	mux.Handle("PATCH /api/admin/users/{id}", admin(s.handleAdminUserUpdate))
	mux.Handle("DELETE /api/admin/users/{id}", admin(s.handleAdminUserDelete))
	mux.Handle("GET /api/admin/dirs", admin(s.handleAdminDirs))
	mux.Handle("GET /api/admin/audit", admin(s.handleAdminAudit))

	mux.Handle("/api/", s.h(func(w http.ResponseWriter, r *http.Request) error {
		return errorf(http.StatusNotFound, "not_found", "unknown API route")
	}))
	mux.Handle("/", s.spaHandler())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		return errorf(http.StatusServiceUnavailable, "db", "database unavailable")
	}
	if _, err := s.base.Stat(""); err != nil {
		return errorf(http.StatusServiceUnavailable, "root", "root folder unavailable")
	}
	writeJSON(w, r, 200, map[string]any{"ok": true, "version": s.version})
	return nil
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, 200, map[string]any{
		"chunkSize":      s.cfg.ChunkSize,
		"batchMaxFiles":  s.cfg.BatchMaxFiles,
		"batchMaxBytes":  s.cfg.BatchMaxBytes,
		"batchFileMax":   1 << 20,
		"maxParallel":    s.cfg.MaxParallel,
		"shareMaxTtl":    int64(s.cfg.ShareMaxTTL.Seconds()),
		"publicUrl":      s.cfg.PublicURL,
		"previewMaxText": 1 << 20,
		"version":        s.version,
	})
	return nil
}
