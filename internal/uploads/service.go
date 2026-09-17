// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

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

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Errors specific to the upload protocol.
var (
	ErrInProgress = errors.New("upload already in progress for this target")
	ErrBadLength  = errors.New("chunk length mismatch")
	ErrBadIndex   = errors.New("chunk index out of range")
	ErrIncomplete = errors.New("upload incomplete")
	ErrNotOwner   = errors.New("not the owner of this upload")
	// ErrReserveExceeded: as sessões abertas do usuário já reservam (fallocate) o teto de bytes.
	ErrReserveExceeded = errors.New("unfinished uploads already reserve too much space")
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
	SentName  string `json:"-"` // nunca sai no JSON: o handler decide qual nome o cliente vê
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// CreateOpts describes a new session. Scope is the base-relative prefix the root is opened at:
// o escopo do usuário no caminho autenticado, a pasta do link no caminho público.
type CreateOpts struct {
	Scope     string
	Dir       string
	Name      string
	UserID    int64  // dono que responde pela reserva de disco (o do link, no modo drop)
	ShareID   int64  // link de envio dono da sessão; 0 = upload autenticado
	Sender    string // visitante anônimo; vazio fora do modo drop
	SentName  string // nome que o visitante pediu, quando difere de Name
	Size      int64
	Mtime     *int64
	Overwrite bool
}

// SessionRef identifies an open session and who is allowed to touch it. No caminho público a
// autorização é o par (link, remetente): um visitante nunca mexe na sessão de outro, e o dono
// do link também não a enxerga.
type SessionRef struct {
	ID      string
	Scope   string
	UserID  int64
	ShareID int64
	Sender  string
}

// Service coordinates sessions between SQLite, the filesystem and in-memory locks.
type Service struct {
	db          *store.DB
	chunkSize   int64
	fsync       bool
	maxReserved int64 // soma dos tamanhos declarados das sessões abertas por usuário; 0 = sem teto
	log         *slog.Logger

	mu       sync.Mutex
	locks    map[string]*sync.Mutex
	writing  map[string]int // blocos em voo por sessão: quem está recebendo bytes agora não é descartado
	createMu sync.Mutex     // conferir o teto e inserir a sessão sem corrida entre duas criações
}

// New creates a service. maxReserved caps the bytes one user's open sessions may reserve (0: no cap).
func New(db *store.DB, chunkSize int64, fsync bool, maxReserved int64, log *slog.Logger) *Service {
	return &Service{db: db, chunkSize: chunkSize, fsync: fsync, maxReserved: maxReserved, log: log,
		locks: map[string]*sync.Mutex{}, writing: map[string]int{}}
}

// beginWrite marks a chunk as in flight for a session and returns the function that clears it.
func (s *Service) beginWrite(id string) func() {
	s.mu.Lock()
	s.writing[id]++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		if s.writing[id]--; s.writing[id] <= 0 {
			delete(s.writing, id)
		}
		s.mu.Unlock()
	}
}

// writingNow reports whether a chunk is being written to the session at this instant.
func (s *Service) writingNow(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writing[id] > 0
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

// MaxUploadSize bounds a single upload (1 PiB) so chunk arithmetic can never overflow.
const MaxUploadSize = int64(1) << 50

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

// ReceivedBytes is how much of the session already reached the disk (chunks marked as received).
func ReceivedBytes(u *store.Upload) int64 {
	n := nchunks(u.Size, u.ChunkSize)
	var total int64
	for i := 0; i < n; i++ {
		if bitSet(u.Received, i) {
			total += chunkLen(u.Size, u.ChunkSize, i)
		}
	}
	return total
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
		Chunks: nchunks(u.Size, u.ChunkSize), Received: received(u), Overwrite: u.Overwrite, SentName: u.SentName, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

// Create starts a session. o.Dir is relative to root, which is opened at o.Scope.
func (s *Service) Create(ctx context.Context, root *vfs.Root, o CreateOpts) (*Info, error) {
	if err := vfs.ValidName(o.Name); err != nil {
		return nil, err
	}
	if o.Size < 0 || o.Size > MaxUploadSize {
		return nil, fmt.Errorf("%w: size out of range", vfs.ErrInvalidPath)
	}
	names := root.NewNamer()
	dir, err := names.MkdirAll(o.Dir)
	if err != nil {
		return nil, err
	}
	o.Dir = dir
	// Nome equivalente já no disco (outra caixa): sem overwrite é conflito na hora, antes de
	// transferir um byte; com overwrite a sessão grava sobre o existente, com o nome dele.
	if cur, err := names.Lookup(o.Dir, o.Name); err != nil {
		return nil, err
	} else if cur != "" {
		if !o.Overwrite {
			return nil, &vfs.NameTakenError{Existing: cur}
		}
		o.Name = cur
	}
	if free := root.DiskFree(); free > 0 && uint64(o.Size) > free {
		return nil, vfs.ErrNoSpace
	}
	id, err := auth.NewID(16)
	if err != nil {
		return nil, err
	}
	u := &store.Upload{ID: id, UserID: o.UserID, ShareID: o.ShareID, Sender: o.Sender, SentName: o.SentName, Dir: vfs.Join(o.Scope, o.Dir), Name: o.Name, Size: o.Size, Mtime: o.Mtime,
		ChunkSize: s.chunkSize, Received: make([]byte, (nchunks(o.Size, s.chunkSize)+7)/8), Overwrite: o.Overwrite}
	if err := s.insert(ctx, u, func(old string) { _ = root.RemovePart(o.Dir, old) }); err != nil {
		return nil, err
	}
	f, err := root.CreatePart(o.Dir, id, o.Size)
	if err != nil {
		_ = s.db.DeleteUpload(ctx, id)
		return nil, err
	}
	f.Close()
	return s.info(u, o.Scope), nil
}

// insert records the session if the user's reservations stay under the cap. A sessão
// pré-aloca o tamanho declarado no disco antes de receber um byte: sem teto, um usuário sem
// cota ocuparia o disco inteiro por até 24 h.
func (s *Service) insert(ctx context.Context, u *store.Upload, dropPart func(id string)) error {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if err := s.supersede(ctx, u, dropPart); err != nil {
		return err
	}
	if s.maxReserved > 0 {
		reserved, err := s.db.ReservedBytes(ctx, u.UserID)
		if err != nil {
			return err
		}
		if reserved+u.Size > s.maxReserved {
			return ErrReserveExceeded
		}
	}
	if err := s.db.CreateUpload(ctx, u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrInProgress
		}
		return err
	}
	return nil
}

// supersede clears an open session that occupies the same target when the client is sending
// another file. Sem isso, um envio interrompido (aba fechada, rede caída) trancava aquele nome
// por até 24 h: reenviar o mesmo vídeo em outra qualidade — outro tamanho, e por isso impossível
// de retomar — falhava com ErrInProgress e não havia saída por item, só descartar todas as
// pendências. Só é descartada a sessão do mesmo dono (e, no modo drop, do mesmo remetente) que
// (a) declara um arquivo diferente, porque tamanho e mtime iguais são uma retomada, que o
// cliente adota, e (b) não está recebendo bloco nenhum neste instante: quem está enviando agora
// tem sempre um PUT em voo, e não perde o que já subiu para outra aba do mesmo usuário.
func (s *Service) supersede(ctx context.Context, u *store.Upload, dropPart func(id string)) error {
	cur, err := s.db.GetUploadTarget(ctx, u.UserID, u.Dir, u.Name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if cur.ShareID != u.ShareID || cur.Sender != u.Sender || !otherFile(cur, u) || s.writingNow(cur.ID) {
		return nil // o insert recusa com ErrInProgress, como antes
	}
	if dropPart != nil {
		dropPart(cur.ID)
	}
	s.unlock(cur.ID)
	if err := s.db.DeleteUpload(ctx, cur.ID); err != nil {
		return err
	}
	s.log.Info("superseded interrupted upload", "id", cur.ID, "dir", cur.Dir, "name", cur.Name, "size", cur.Size, "newSize", u.Size)
	return nil
}

// otherFile reports whether b certainly describes bytes different from a's, pelo mesmo par com
// que o cliente casa uma sessão pendente: tamanho e mtime (tolerância de 1 s, como em
// manager.ts). Sem mtime dos dois lados não dá para afirmar nada, e a dúvida preserva a sessão.
func otherFile(a, b *store.Upload) bool {
	if a.Size != b.Size {
		return true
	}
	if a.Mtime == nil || b.Mtime == nil {
		return false
	}
	d := *a.Mtime - *b.Mtime
	return d <= -1000 || d >= 1000
}

// load fetches a session and checks that the caller may touch it. Toda falha vira ErrNotFound:
// quem não é o dono da sessão não deve nem saber que ela existe.
func (s *Service) load(ctx context.Context, ref SessionRef) (*store.Upload, error) {
	u, err := s.db.GetUpload(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	if ref.ShareID != 0 {
		if u.ShareID != ref.ShareID || ref.Sender == "" || u.Sender != ref.Sender {
			return nil, store.ErrNotFound
		}
		return u, nil
	}
	// Caminho autenticado: a sessão de um link de envio não pertence a ninguém logado.
	if u.ShareID != 0 || u.UserID != ref.UserID {
		return nil, store.ErrNotFound
	}
	return u, nil
}

// Get returns a session the caller owns.
func (s *Service) Get(ctx context.Context, ref SessionRef) (*Info, error) {
	u, err := s.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	if _, ok := relDir(u, ref.Scope); !ok {
		return nil, store.ErrNotFound
	}
	return s.info(u, ref.Scope), nil
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
func (s *Service) WriteChunk(ctx context.Context, root *vfs.Root, ref SessionRef, index int, length int64, body io.Reader) (*Info, error) {
	id, scope := ref.ID, ref.Scope
	// Marcado antes de carregar a sessão: enquanto este bloco estiver em voo, uma criação
	// concorrente para o mesmo destino não descarta a sessão debaixo dele (supersede).
	defer s.beginWrite(id)()
	u, err := s.load(ctx, ref)
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
	cur, err := s.load(ctx, ref)
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
func (s *Service) Complete(ctx context.Context, root *vfs.Root, ref SessionRef) (*vfs.Entry, []int, error) {
	id, scope := ref.ID, ref.Scope
	m := s.lock(id)
	m.Lock()
	defer m.Unlock()
	u, err := s.load(ctx, ref)
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
	name, err := root.Finalize(dir, id, u.Name, u.Overwrite, nil)
	if err != nil {
		return nil, nil, err
	}
	// A partir daqui o arquivo já existe com o nome final: desistir agora deixaria a sessão viva
	// no banco reservando cota de um arquivo que foi entregue. WithoutCancel garante que a baixa
	// acontece mesmo que quem enviou tenha fechado a aba no exato instante da finalização.
	_ = s.db.DeleteUpload(context.WithoutCancel(ctx), id)
	s.unlock(id)
	e, err := root.Stat(vfs.Join(dir, name))
	if err != nil {
		return nil, nil, err
	}
	return e, nil, nil
}

// Abort deletes the session and its part file.
func (s *Service) Abort(ctx context.Context, root *vfs.Root, ref SessionRef) error {
	id := ref.ID
	u, err := s.load(ctx, ref)
	if err != nil {
		return err
	}
	dir, ok := relDir(u, ref.Scope)
	if !ok {
		return store.ErrNotFound
	}
	_ = root.RemovePart(dir, id)
	s.unlock(id)
	return s.db.DeleteUpload(ctx, id)
}

// CleanupStale removes idle sessions using the base root. Sessões de link de envio usam
// dropMaxAge, bem mais curto: elas seguram a cota do link e foram abertas por anônimos.
func (s *Service) CleanupStale(ctx context.Context, base *vfs.Root, maxAge, dropMaxAge time.Duration) {
	now := time.Now()
	stale, err := s.db.ListStaleUploads(ctx, now.Add(-maxAge).Unix(), now.Add(-dropMaxAge).Unix())
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
	cutoff := time.Now().Add(-OrphanGrace)
	for _, name := range parts {
		id, ok := strings.CutPrefix(name, vfs.ReservedPrefix+"upload-")
		if !ok {
			continue
		}
		id = strings.TrimSuffix(id, ".part")
		if active[id] {
			continue
		}
		// Partes de PUT único e de lote não têm linha no banco: enquanto o corpo chega o
		// mtime avança, então só uma parte parada há mais de OrphanGrace é órfã de verdade.
		if fi, err := root.StatReserved(dir, name); err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		if err := root.RemoveReserved(dir, name); err == nil {
			s.log.Info("removed orphan part", "dir", dir, "name", name)
		}
	}
}

// OrphanGrace is how long a part file without an upload session may sit untouched
// before the listing sweep removes it. Single-PUT and batch uploads write their part
// without a session row; their mtime keeps moving while the body streams in.
const OrphanGrace = 15 * time.Minute

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
