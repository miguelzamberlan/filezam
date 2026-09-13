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

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/config"
	"github.com/miguelzamberlan/filezam/internal/index"
	"github.com/miguelzamberlan/filezam/internal/jobs"
	"github.com/miguelzamberlan/filezam/internal/metrics"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/thumbs"
	"github.com/miguelzamberlan/filezam/internal/uploads"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Server holds all dependencies.
type Server struct {
	cfg       *config.Config
	db        *store.DB
	base      *vfs.Root
	uploads   *uploads.Service
	jobs      *jobs.Manager
	indexer   *index.Indexer // nil quando FILEZAM_INDEX_INTERVAL=0
	indexWake chan struct{}  // avisa o laço do índice que o intervalo mudou
	started   time.Time      // subida do processo: separa o que um processo anterior deixou pela metade
	metrics   *metrics.Registry
	log       *slog.Logger
	version   string

	loginIP     *auth.Limiter
	loginUser   *auth.Limiter
	loginSem    auth.Semaphore
	publicIP    *auth.Limiter
	publicDL    *auth.KeyedSemaphore
	publicWait  time.Duration        // quanto uma requisição pública espera por um slot de publicDL
	publicZip   auth.Semaphore       // zips públicos simultâneos no servidor inteiro
	dropIP      *auth.Limiter        // escrita anônima: balde próprio, o de leitura é apertado demais
	dropSem     *auth.KeyedSemaphore // envios anônimos simultâneos por IP
	dropMu      dropLocks            // serializa a admissão de arquivos por link
	extractSem  *auth.KeyedSemaphore // uma extração/compactação por usuário
	archiveSem  auth.Semaphore       // extrações simultâneas no servidor inteiro
	thumbSem    *auth.KeyedSemaphore // gerações de miniatura simultâneas por usuário
	thumbs      *thumbs.Cache        // nil quando as miniaturas estão desligadas ou o cache falhou
	slugMiss    *auth.Limiter        // tentativas malsucedidas com forma de apelido
	uploadSem   *auth.KeyedSemaphore
	searchSem   *auth.KeyedSemaphore
	shareUnlock *auth.Limiter        // tentativas de senha por link
	lockout     *auth.Lockout        // bloqueio por (usuário, IP) após falhas seguidas
	zipSem      *auth.KeyedSemaphore // zips autenticados simultâneos por usuário
	secretKey   []byte               // cifra dos segredos TOTP e HMAC do cookie de dispositivo confiável
	pending     pendingState         // logins à espera do código TOTP e cadastros em andamento
	quota       quotaCache
	set         settingsCache // configurações globais editadas pelo admin (migração 011)
	adminMu     sync.Mutex    // serializa alterações de usuários: a checagem de "último admin" não é atômica no banco

	bg     context.Context
	cancel context.CancelFunc
}

// New wires the server. It ensures the first admin exists.
func New(cfg *config.Config, db *store.DB, base *vfs.Root, log *slog.Logger, version string) (*Server, error) {
	bg, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg: cfg, db: db, base: base, log: log, version: version,
		uploads:     uploads.New(db, cfg.ChunkSize, cfg.Fsync, cfg.UploadReserve, log),
		jobs:        jobs.New(bg),
		metrics:     metrics.New(),
		loginIP:     auth.NewLimiter(10, 10),
		loginUser:   auth.NewLimiter(5, 5),
		loginSem:    auth.NewSemaphore(4),
		publicIP:    auth.NewLimiter(120, 120),
		publicDL:    auth.NewKeyedSemaphore(2),
		publicWait:  30 * time.Second,
		publicZip:   auth.NewSemaphore(publicZipMax),
		dropIP:      auth.NewLimiter(900, 240),
		dropSem:     auth.NewKeyedSemaphore(dropClientParallel),
		extractSem:  auth.NewKeyedSemaphore(1),
		archiveSem:  auth.NewSemaphore(extractGlobalMax),
		thumbSem:    auth.NewKeyedSemaphore(4),
		slugMiss:    auth.NewLimiter(30, 30),
		uploadSem:   auth.NewKeyedSemaphore(8),
		searchSem:   auth.NewKeyedSemaphore(2),
		shareUnlock: auth.NewLimiter(5, 5),
		lockout:     auth.NewLockout(lockoutThreshold, lockoutBase),
		zipSem:      auth.NewKeyedSemaphore(2),
		quota:       quotaCache{m: map[int64]usageEntry{}},
		bg:          bg, cancel: cancel,
	}
	key, err := loadSecretKey(cfg)
	if err != nil {
		cancel()
		return nil, err
	}
	s.secretKey = key
	s.pending = newPendingState()
	s.indexWake = make(chan struct{}, 1)
	s.started = time.Now()
	// O cache de miniaturas vive no DataDir, fora da árvore do usuário: não entra em backup de
	// conteúdo, não aparece em listagem nem em zip, e segue o precedente do banco e do secret.key.
	if tc, err := thumbs.New(filepath.Join(cfg.DataDir, "thumbs"), log); err != nil {
		log.Warn("thumbnail cache unavailable; thumbnails disabled", "err", err)
	} else {
		s.thumbs = tc
	}
	s.set.v = defaultSettings()
	if err := s.loadSettings(bg); err != nil {
		cancel()
		return nil, err
	}
	if cfg.IndexInterval > 0 {
		s.indexer = index.New(db, base, log, s.indexInterval)
	}
	// histórico de jobs: snapshots vão para o SQLite; o que ficou "running" de um processo anterior é marcado como interrompido
	s.jobs.Persist = func(userID int64, v jobs.View) {
		rec := &store.JobRecord{ID: v.ID, UserID: userID, Type: v.Type, Label: v.Label, State: v.State, Done: v.Done, Total: v.Total, BytesDone: v.BytesDone, BytesTotal: v.BytesTotal, Error: v.Error, Warnings: len(v.Warnings), StartedAt: v.StartedAt}
		if v.FinishedAt > 0 {
			rec.FinishedAt = &v.FinishedAt
			s.metrics.Inc("filezam_jobs_finished_total", `type="`+v.Type+`",state="`+v.State+`"`, 1)
		}
		if err := db.UpsertJob(context.Background(), rec); err != nil {
			log.Warn("persist job", "id", v.ID, "err", err)
		}
	}
	if n, err := db.MarkInterruptedJobs(bg); err == nil && n > 0 {
		log.Warn("jobs interrupted by a previous restart", "count", n)
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
	// Sem FILEZAM_ADMIN_PASSWORD a senha é sorteada e sai no log uma única vez. É a troca
	// deliberada: uma senha no log de quem já é dono da máquina, em vez de um "admin/admin" que
	// vale desde a subida do serviço até o primeiro login e que qualquer varredura conhece. A
	// troca continua obrigatória no primeiro acesso, então mesmo essa linha de log envelhece.
	pw, generated := s.cfg.AdminPassword, false
	if pw == "" {
		if pw, err = auth.NewReadablePassword(); err != nil {
			return err
		}
		generated = true
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	_, err = s.db.CreateUser(ctx, &store.User{Username: s.cfg.AdminUser, PasswordHash: hash, Role: "admin", MustChangePassword: true})
	if err != nil {
		return fmt.Errorf("create initial admin: %w", err)
	}
	if generated {
		s.log.Warn("created initial admin user with a random password, shown here only once; change it at first login (required)",
			"username", s.cfg.AdminUser, "password", pw)
	} else {
		s.log.Warn("created initial admin user from FILEZAM_ADMIN_PASSWORD; password change is required at first login", "username", s.cfg.AdminUser)
	}
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
		s.uploads.CleanupStale(ctx, s.base, s.cfg.UploadStaleAge, time.Duration(s.settings().DropStaleAge)*time.Second)
		if err := s.db.PurgeExpiredSessions(ctx); err != nil {
			s.log.Warn("purge sessions", "err", err)
		}
		if err := s.db.PruneAudit(ctx, time.Now().Add(-180*24*time.Hour).Unix()); err != nil {
			s.log.Warn("prune audit", "err", err)
		}
		s.recoverInterruptedJobs(ctx)
		s.recoverPendingTrash(ctx)
		s.sweepBrokenShares(ctx)
		s.sweepTrash(ctx)
		if err := s.db.PruneJobs(ctx, time.Now().Add(-30*24*time.Hour).Unix()); err != nil {
			s.log.Warn("prune jobs", "err", err)
		}
		if err := s.db.PruneNotifications(ctx, time.Now().Add(-notificationsKeep).Unix()); err != nil {
			s.log.Warn("prune notifications", "err", err)
		}
		// A chave do cache muda a cada alteração do arquivo, então as entradas velhas ficam para
		// trás por construção: a varredura recolhe as mais antigas quando o teto é passado.
		if s.thumbs != nil {
			s.thumbs.Prune(s.settings().ThumbsCacheMax)
		}
	}
	if s.indexer != nil {
		go func() {
			scan := func() {
				if _, err := s.indexer.FullScan(s.bg); err != nil {
					s.log.Warn("index scan", "err", err)
				}
			}
			// O intervalo vem das configurações e pode mudar a qualquer momento: a próxima
			// varredura é marcada a partir do fim da anterior, e mudar o valor no painel
			// (indexWake) remarca na hora — encurtar para menos do que já passou varre já.
			scan()
			last := time.Now()
			t := time.NewTimer(s.indexInterval())
			defer t.Stop()
			for {
				select {
				case <-s.bg.Done():
					return
				case <-s.indexWake:
					t.Stop()
					t.Reset(max(0, time.Until(last.Add(s.indexInterval()))))
				case <-t.C:
					scan()
					last = time.Now()
					t.Reset(s.indexInterval())
				}
			}
		}()
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
	mux.Handle("GET /metrics", s.h(s.handleMetrics))
	mux.Handle("GET /api/config", authed(s.handleConfig))

	mux.Handle("POST /api/auth/login", chain(s.h(s.handleLogin), s.csrf))
	mux.Handle("POST /api/auth/logout", authed(s.handleLogout))
	mux.Handle("GET /api/auth/me", authed(s.handleMe))
	mux.Handle("POST /api/auth/password", authed(s.handleChangePassword))
	mux.Handle("POST /api/auth/totp", chain(s.h(s.handleLoginTOTP), s.csrf))
	mux.Handle("POST /api/auth/totp/setup", authed(s.handleTOTPSetup))
	mux.Handle("POST /api/auth/totp/enable", authed(s.handleTOTPEnable))
	mux.Handle("POST /api/auth/totp/disable", authed(s.handleTOTPDisable))
	mux.Handle("POST /api/auth/totp/recovery", authed(s.handleTOTPRecovery))

	mux.Handle("GET /api/files", user(s.handleList))
	mux.Handle("GET /api/files/stat", user(s.handleStat))
	mux.Handle("GET /api/files/info", user(s.handleInfo))
	mux.Handle("GET /api/files/disk", user(s.handleDisk))
	mux.Handle("GET /api/files/search", user(s.handleSearch))
	mux.Handle("GET /api/files/content", user(s.handleContent))
	mux.Handle("GET /api/files/thumb", user(s.handleThumb))
	mux.Handle("PUT /api/files/content", user(s.handlePutContent))
	mux.Handle("POST /api/files/batch", user(s.handleBatch))
	mux.Handle("GET /api/files/zip", user(s.handleZip))
	mux.Handle("GET /api/files/zip/plan", user(s.handleZipPlan))
	mux.Handle("POST /api/files/mkdir", user(s.handleMkdir))
	mux.Handle("POST /api/files/rename", user(s.handleRename))
	mux.Handle("POST /api/files/delete", user(s.handleDelete))
	mux.Handle("POST /api/files/copy", user(s.handleCopy))
	mux.Handle("POST /api/files/move", user(s.handleMove))
	mux.Handle("POST /api/files/extract", user(s.handleExtract))
	mux.Handle("POST /api/files/archive", user(s.handleArchive))

	mux.Handle("GET /api/notifications", user(s.handleNotifications))
	mux.Handle("GET /api/notifications/unread", user(s.handleNotificationsUnread))
	mux.Handle("POST /api/notifications/read", user(s.handleNotificationsRead))
	mux.Handle("DELETE /api/notifications/{id}", user(s.handleNotificationDelete))
	mux.Handle("GET /api/jobs", user(s.handleJobs))
	mux.Handle("GET /api/jobs/history", user(s.handleJobHistory))
	mux.Handle("GET /api/jobs/{id}", user(s.handleJob))
	mux.Handle("DELETE /api/jobs/{id}", user(s.handleJobCancel))

	mux.Handle("POST /api/uploads", user(s.handleUploadCreate))
	mux.Handle("GET /api/uploads", user(s.handleUploadList))
	mux.Handle("GET /api/uploads/{id}", user(s.handleUploadGet))
	mux.Handle("PUT /api/uploads/{id}", user(s.handleUploadChunk))
	mux.Handle("POST /api/uploads/{id}/complete", user(s.handleUploadComplete))
	mux.Handle("DELETE /api/uploads/{id}", user(s.handleUploadAbort))

	mux.Handle("GET /api/trash", user(s.handleTrashList))
	mux.Handle("POST /api/trash/restore", user(s.handleTrashRestore))
	mux.Handle("POST /api/trash/delete", user(s.handleTrashDelete))
	mux.Handle("POST /api/trash/empty", user(s.handleTrashEmpty))

	mux.Handle("GET /api/favorites", user(s.handleFavorites))
	mux.Handle("POST /api/favorites", user(s.handleFavoriteAdd))
	mux.Handle("DELETE /api/favorites/{id}", user(s.handleFavoriteDelete))

	mux.Handle("GET /api/shares", user(s.handleShares))
	mux.Handle("POST /api/shares", user(s.handleShareCreate))
	mux.Handle("POST /api/shares/affected", user(s.handleSharesAffected))
	mux.Handle("DELETE /api/shares/{id}", user(s.handleShareDelete))

	mux.Handle("GET /api/public/{token}", s.h(s.handlePublicInfo))
	mux.Handle("GET /api/public/{token}/list", s.h(s.handlePublicList))
	mux.Handle("GET /api/public/{token}/content", s.h(s.handlePublicContent))
	mux.Handle("GET /api/public/{token}/zip", s.h(s.handlePublicZip))
	mux.Handle("GET /api/public/{token}/zip/plan", s.h(s.handlePublicZipPlan))
	mux.Handle("POST /api/public/{token}/unlock", chain(s.h(s.handlePublicUnlock), s.csrf))

	// Envio anônimo: mesmo padrão do unlock — CSRF sem sessão. A autorização é o token (ou o
	// apelido mais a senha) e o modo do link, nunca um middleware de usuário.
	mux.Handle("PUT /api/public/{token}/content", chain(s.h(s.handleDropPut), s.csrf))
	mux.Handle("POST /api/public/{token}/uploads", chain(s.h(s.handleDropUploadCreate), s.csrf))
	mux.Handle("PUT /api/public/{token}/uploads/{id}", chain(s.h(s.handleDropUploadChunk), s.csrf))
	mux.Handle("POST /api/public/{token}/uploads/{id}/complete", chain(s.h(s.handleDropUploadComplete), s.csrf))
	mux.Handle("DELETE /api/public/{token}/uploads/{id}", chain(s.h(s.handleDropUploadAbort), s.csrf))

	mux.Handle("GET /api/admin/users", admin(s.handleAdminUsers))
	mux.Handle("POST /api/admin/users", admin(s.handleAdminUserCreate))
	mux.Handle("PATCH /api/admin/users/{id}", admin(s.handleAdminUserUpdate))
	mux.Handle("DELETE /api/admin/users/{id}", admin(s.handleAdminUserDelete))
	mux.Handle("POST /api/admin/users/{id}/totp/reset", admin(s.handleAdminTOTPReset))
	mux.Handle("GET /api/admin/settings", admin(s.handleAdminSettings))
	mux.Handle("PATCH /api/admin/settings", admin(s.handleAdminSettingsUpdate))
	mux.Handle("GET /api/admin/dirs", admin(s.handleAdminDirs))
	mux.Handle("GET /api/admin/audit", admin(s.handleAdminAudit))
	mux.Handle("GET /api/admin/index", admin(s.handleAdminIndex))
	mux.Handle("POST /api/admin/reindex", admin(s.handleAdminReindex))

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
		"trashRetention": int64(s.trashRetention().Seconds()),
		"require2fa":     s.cfg.Require2FA,
		"slugsEnabled":   s.settings().SlugsEnabled,
		"dropEnabled":    s.settings().DropEnabled,
		"dropMaxTtl":     s.dropMaxTTL(),
		"dropMaxQuota":   s.settings().DropMaxQuota,
		"dropFileMax":    s.settings().DropFileMax,
		"dropMaxFiles":   s.settings().DropMaxFiles,
		"extractEnabled": s.settings().ExtractEnabled,
		"thumbsEnabled":  s.settings().ThumbsEnabled && s.thumbs != nil,
		"previewMaxText": 1 << 20,
		"version":        s.version,
	})
	return nil
}
