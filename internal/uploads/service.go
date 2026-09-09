// Package uploads manages resumable chunked upload sessions.
package uploads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
)

// Errors specific to the upload protocol.
var (
	ErrInProgress = errors.New("upload already in progress for this target")
	ErrBadLength  = errors.New("chunk length mismatch")
	ErrBadIndex   = errors.New("chunk index out of range")
	ErrIncomplete = errors.New("upload incomplete")
	ErrNotOwner   = errors.New("not the owner of this upload")
)

// Info is the JSON view of a session.
type Info struct {
	ID        string `json:"id"`
	Dir       string `json:"dir"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Mtime     *int64 `json:"mtime"`
	ChunkSize int64  `json:"chunkSize"`
	Chunks    int    `json:"chunks"`
	Received  []int  `json:"received"`
	Overwrite bool   `json:"overwrite"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Service coordinates sessions between SQLite, the filesystem and in-memory locks.
type Service struct {
	db        *store.DB
	chunkSize int64
	fsync     bool
	log       *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New creates a service.
func New(db *store.DB, chunkSize int64, fsync bool, log *slog.Logger) *Service {
	return &Service{db: db, chunkSize: chunkSize, fsync: fsync, log: log, locks: map[string]*sync.Mutex{}}
}

// ChunkSize returns the configured chunk size.
func (s *Service) ChunkSize() int64 { return s.chunkSize }

func (s *Service) lock(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.locks[id]
	if !ok {
		m = &sync.Mutex{}
		s.locks[id] = m
	}
	return m
}

func (s *Service) unlock(id string) {
	s.mu.Lock()
	delete(s.locks, id)
	s.mu.Unlock()
}

func nchunks(size, chunk int64) int {
	if size == 0 {
		return 0
	}
	return int((size + chunk - 1) / chunk)
}

func chunkLen(size, chunk int64, index int) int64 {
	off := int64(index) * chunk
	if off+chunk > size {
		return size - off
	}
	return chunk
}

func bitSet(bits []byte, i int) bool { return bits[i/8]&(1<<(i%8)) != 0 }
func setBit(bits []byte, i int)      { bits[i/8] |= 1 << (i % 8) }

func received(u *store.Upload) []int {
	n := nchunks(u.Size, u.ChunkSize)
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if bitSet(u.Received, i) {
			out = append(out, i)
		}
	}
	return out
}

func missing(u *store.Upload) []int {
	n := nchunks(u.Size, u.ChunkSize)
	var out []int
	for i := 0; i < n; i++ {
		if !bitSet(u.Received, i) {
			out = append(out, i)
		}
	}
	return out
}

// relDir converts the stored base-relative dir into a scope-relative dir.
func relDir(u *store.Upload, scope string) (string, bool) {
	if scope == "" {
		return u.Dir, true
	}
	if u.Dir == scope {
		return "", true
	}
	if strings.HasPrefix(u.Dir, scope+"/") {
		return u.Dir[len(scope)+1:], true
	}
	return "", false
}

func (s *Service) info(u *store.Upload, scope string) *Info {
	dir, _ := relDir(u, scope)
	return &Info{ID: u.ID, Dir: dir, Name: u.Name, Size: u.Size, Mtime: u.Mtime, ChunkSize: u.ChunkSize,
		Chunks: nchunks(u.Size, u.ChunkSize), Received: received(u), Overwrite: u.Overwrite, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

// Create starts a session. dir is scope-relative and normalized; root is the user's scope root.
func (s *Service) Create(ctx context.Context, root *vfs.Root, scope string, userID int64, dir, name string, size int64, mtime *int64, overwrite bool) (*Info, error) {
	if err := vfs.ValidName(name); err != nil {
		return nil, err
	}
	if size < 0 {
		return nil, fmt.Errorf("%w: negative size", vfs.ErrInvalidPath)
	}
	if err := root.MkdirAll(dir); err != nil {
		return nil, err
	}
	target := vfs.Join(dir, name)
	if exists, err := root.Exists(target); err != nil {
		return nil, err
	} else if exists && !overwrite {
		return nil, fmt.Errorf("%w: %s", vfs.ErrExists, name)
	}
	if free := root.DiskFree(); free > 0 && uint64(size) > free {
		return nil, vfs.ErrNoSpace
	}
	id, err := auth.NewID(16)
	if err != nil {
		return nil, err
	}
	u := &store.Upload{ID: id, UserID: userID, Dir: vfs.Join(scope, dir), Name: name, Size: size, Mtime: mtime,
		ChunkSize: s.chunkSize, Received: make([]byte, (nchunks(size, s.chunkSize)+7)/8), Overwrite: overwrite}
	if err := s.db.CreateUpload(ctx, u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrInProgress
		}
		return nil, err
	}
	f, err := root.CreatePart(dir, id, size)
	if err != nil {
		_ = s.db.DeleteUpload(ctx, id)
		return nil, err
	}
	f.Close()
	return s.info(u, scope), nil
}

func (s *Service) load(ctx context.Context, userID int64, id string) (*store.Upload, error) {
	u, err := s.db.GetUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.UserID != userID {
		return nil, store.ErrNotFound
	}
	return u, nil
}

// Get returns a session owned by the user.
func (s *Service) Get(ctx context.Context, userID int64, scope, id string) (*Info, error) {
	u, err := s.load(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if _, ok := relDir(u, scope); !ok {
		return nil, store.ErrNotFound
	}
	return s.info(u, scope), nil
}

// List returns the user's pending sessions inside the scope.
func (s *Service) List(ctx context.Context, userID int64, scope string) ([]*Info, error) {
	us, err := s.db.ListUploads(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []*Info{}
	for _, u := range us {
		if _, ok := relDir(u, scope); ok {
			out = append(out, s.info(u, scope))
		}
	}
	return out, nil
}

// WriteChunk stores chunk `index` from body, which must be exactly the expected length.
func (s *Service) WriteChunk(ctx context.Context, root *vfs.Root, scope string, userID int64, id string, index int, length int64, body io.Reader) (*Info, error) {
	u, err := s.load(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	dir, ok := relDir(u, scope)
	if !ok {
		return nil, store.ErrNotFound
	}
	n := nchunks(u.Size, u.ChunkSize)
	if index < 0 || index >= n {
		return nil, ErrBadIndex
	}
	want := chunkLen(u.Size, u.ChunkSize, index)
	if length != want {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrBadLength, want, length)
	}
	f, err := root.OpenPart(dir, id)
	if err != nil {
		return nil, err
	}
	w := io.NewOffsetWriter(f, int64(index)*u.ChunkSize)
	copied, err := io.Copy(w, io.LimitReader(body, want))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, vfs.MapError(err)
	}
	if copied != want {
		return nil, fmt.Errorf("%w: body ended after %d bytes", ErrBadLength, copied)
	}
	m := s.lock(id)
	m.Lock()
	defer m.Unlock()
	cur, err := s.load(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	setBit(cur.Received, index)
	if err := s.db.UpdateUploadReceived(ctx, id, cur.Received); err != nil {
		return nil, err
	}
	return s.info(cur, scope), nil
}

// Complete finalizes a session. Returns the missing chunk indexes with ErrIncomplete if not done.
func (s *Service) Complete(ctx context.Context, root *vfs.Root, scope string, userID int64, id string) (*vfs.Entry, []int, error) {
	m := s.lock(id)
	m.Lock()
	defer m.Unlock()
	u, err := s.load(ctx, userID, id)
	if err != nil {
		return nil, nil, err
	}
	dir, ok := relDir(u, scope)
	if !ok {
		return nil, nil, store.ErrNotFound
	}
	if miss := missing(u); len(miss) > 0 {
		return nil, miss, ErrIncomplete
	}
	if s.fsync {
		f, err := root.OpenPart(dir, id)
		if err != nil {
			return nil, nil, err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return nil, nil, vfs.MapError(err)
		}
	}
	if u.Mtime != nil {
		_ = root.Chtimes(vfs.Join(dir, vfs.PartName(id)), clampMtime(*u.Mtime))
	}
	if err := root.Finalize(dir, id, u.Name, u.Overwrite); err != nil {
		return nil, nil, err
	}
	_ = s.db.DeleteUpload(ctx, id)
	s.unlock(id)
	e, err := root.Stat(vfs.Join(dir, u.Name))
	if err != nil {
		return nil, nil, err
	}
	return e, nil, nil
}

// Abort deletes the session and its part file.
func (s *Service) Abort(ctx context.Context, root *vfs.Root, scope string, userID int64, id string) error {
	u, err := s.load(ctx, userID, id)
	if err != nil {
		return err
	}
	dir, ok := relDir(u, scope)
	if !ok {
		return store.ErrNotFound
	}
	_ = root.RemovePart(dir, id)
	s.unlock(id)
	return s.db.DeleteUpload(ctx, id)
}

// CleanupStale removes sessions idle longer than maxAge using the base root.
func (s *Service) CleanupStale(ctx context.Context, base *vfs.Root, maxAge time.Duration) {
	stale, err := s.db.ListStaleUploads(ctx, time.Now().Add(-maxAge).Unix())
	if err != nil {
		s.log.Warn("upload cleanup query failed", "err", err)
		return
	}
	for _, u := range stale {
		if err := base.RemovePart(u.Dir, u.ID); err != nil {
			s.log.Warn("upload cleanup: remove part", "id", u.ID, "err", err)
		}
		_ = s.db.DeleteUpload(ctx, u.ID)
		s.unlock(u.ID)
		s.log.Info("removed stale upload", "id", u.ID, "dir", u.Dir, "name", u.Name)
	}
}

// PruneOrphans deletes part files in a listing that belong to no session.
func (s *Service) PruneOrphans(ctx context.Context, root *vfs.Root, dir string, parts []string) {
	if len(parts) == 0 {
		return
	}
	active, err := s.db.ActiveUploadIDs(ctx)
	if err != nil {
		return
	}
	for _, name := range parts {
		id, ok := strings.CutPrefix(name, vfs.ReservedPrefix+"upload-")
		if !ok {
			continue
		}
		id = strings.TrimSuffix(id, ".part")
		if active[id] {
			continue
		}
		if err := root.RemoveReserved(dir, name); err == nil {
			s.log.Info("removed orphan part", "dir", dir, "name", name)
		}
	}
}

// ClampMtime bounds a client-supplied unix-ms mtime to a sane range.
func clampMtime(ms int64) time.Time {
	t := time.UnixMilli(ms)
	if ms <= 0 {
		return time.Now()
	}
	if max := time.Now().Add(24 * time.Hour); t.After(max) {
		return max
	}
	return t
}

// ClampMtime is exported for the single-PUT and batch paths.
func ClampMtime(ms int64) time.Time { return clampMtime(ms) }
