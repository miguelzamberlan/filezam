package server

import (
	"context"
	"fmt"
	"github.com/zamberlan/filezam/internal/jobs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
)

// Cota por usuário: o uso é o total de bytes dentro do escopo (lixeira e partes não contam).
// Vem do índice de nomes quando pronto (uma soma em SQL); senão de uma varredura limitada.
// O valor fica em cache por 30 s e é ajustado localmente a cada upload/cópia aceitos, para
// vários envios seguidos não passarem do limite entre duas medições.

const (
	quotaCacheTTL  = 30 * time.Second
	maxJobsPerUser = 4
)

type usageEntry struct {
	used int64
	at   time.Time
}

type quotaCache struct {
	mu sync.Mutex
	m  map[int64]usageEntry
}

type jobsJob = jobs.Job

var errQuota = errorf(http.StatusInsufficientStorage, "quota_exceeded", "disk quota exceeded")

// usage returns the bytes the user currently occupies (cached).
func (s *Server) usage(ctx context.Context, u *store.User) (int64, error) {
	s.quota.mu.Lock()
	if e, ok := s.quota.m[u.ID]; ok && time.Since(e.at) < quotaCacheTTL {
		s.quota.mu.Unlock()
		return e.used, nil
	}
	s.quota.mu.Unlock()
	var used int64
	if s.indexer != nil && s.indexer.Ready() {
		n, err := s.db.IndexSumSize(ctx, u.Scope)
		if err != nil {
			return 0, err
		}
		used = n
	} else {
		root, err := s.scopeRoot(u)
		if err != nil {
			return 0, err
		}
		t, _ := root.ScanLimited(ctx, "", infoScanLimit) // parcial em árvores enormes: melhor subestimar que bloquear
		root.Close()
		used = t.Bytes
	}
	s.quota.mu.Lock()
	s.quota.m[u.ID] = usageEntry{used: used, at: time.Now()}
	s.quota.mu.Unlock()
	return used, nil
}

// usageAdd adjusts the cached usage after an accepted write (may go negative on deletes; clamped).
func (s *Server) usageAdd(userID, delta int64) {
	s.quota.mu.Lock()
	if e, ok := s.quota.m[userID]; ok {
		e.used += delta
		if e.used < 0 {
			e.used = 0
		}
		s.quota.m[userID] = e
	}
	s.quota.mu.Unlock()
}

// checkQuota refuses a write of `incoming` bytes that would exceed the user's quota.
func (s *Server) checkQuota(ctx context.Context, u *store.User, incoming int64) error {
	if u.Quota <= 0 {
		return nil
	}
	used, err := s.usage(ctx, u)
	if err != nil {
		return err
	}
	if used+incoming > u.Quota {
		return errQuota
	}
	return nil
}

// startJob enforces the per-user cap on running background jobs.
func (s *Server) startJob(u *store.User, typ, label string, dirs []string, fn func(ctx context.Context, j *jobsJob) error) (*jobsJob, error) {
	if s.jobs.Running(u.ID) >= maxJobsPerUser {
		return nil, errorf(http.StatusTooManyRequests, "busy", "too many operations running; wait for one to finish")
	}
	return s.jobs.StartLabeled(u.ID, typ, label, dirs, fn), nil
}

// jobLabel summarises the items of an operation for the history ("a.txt, b.txt +3 → Fotos").
func jobLabel(paths []string, dest string) string {
	names := make([]string, 0, 2)
	for i, p := range paths {
		if i == 2 {
			break
		}
		names = append(names, vfs.Base(p))
	}
	label := strings.Join(names, ", ")
	if len(paths) > 2 {
		label += fmt.Sprintf(" +%d", len(paths)-2)
	}
	if dest != "" {
		label += " → /" + dest
	}
	return label
}

// quotaView is the per-user part of /api/files/disk.
func (s *Server) quotaView(ctx context.Context, u *store.User) map[string]any {
	if u.Quota <= 0 {
		return nil
	}
	used, err := s.usage(ctx, u)
	if err != nil {
		return nil
	}
	return map[string]any{"quota": u.Quota, "quotaUsed": used}
}

// scanBytes sums the regular file bytes of paths under root (for copy admission).
func scanBytes(ctx context.Context, root *vfs.Root, paths []string) (int64, error) {
	var n int64
	for _, p := range paths {
		t, err := root.Scan(ctx, p)
		if err != nil {
			return 0, err
		}
		n += t.Bytes
	}
	return n, nil
}
