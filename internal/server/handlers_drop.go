package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/uploads"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// dropOpenPerSender limita quantas sessões em blocos um visitante mantém abertas no mesmo
// link. A cota limita bytes, não inodes: sem este teto, milhares de sessões de 1 byte caberiam
// na cota e encheriam o diretório de arquivos .part.
const dropOpenPerSender = 8

// dropCookieName carries the visitor's identity on one link. Não é autenticação: serve só para
// o visitante reencontrar os próprios envios.
func dropCookieName(sh *store.Share) string { return "fz_d_" + sh.TokenHash[:16] }

// dropSenderValue assina o id do remetente com a chave do servidor. Sem assinatura, `sender`
// seria texto arbitrário escolhido pelo cliente: daria para gravar lixo no banco e para sondar
// a lista de envios de outro visitante chutando ids.
func (s *Server) dropSenderValue(sh *store.Share, id string) string {
	m := hmac.New(sha256.New, s.secretKey)
	m.Write([]byte(sh.TokenHash))
	m.Write([]byte("|"))
	m.Write([]byte(id))
	return id + "." + hex.EncodeToString(m.Sum(nil))
}

// dropSender returns the visitor's id from the cookie, or "" when there is none that checks out.
func (s *Server) dropSender(r *http.Request, sh *store.Share) string {
	c, err := r.Cookie(dropCookieName(sh))
	if err != nil {
		return ""
	}
	id, _, ok := strings.Cut(c.Value, ".")
	if !ok || len(id) != 32 {
		return ""
	}
	if !hmac.Equal([]byte(c.Value), []byte(s.dropSenderValue(sh, id))) {
		return ""
	}
	return id
}

// dropSenderOrNew returns the visitor's id, minting and setting a fresh one when the cookie is
// missing, expired or tampered with.
func (s *Server) dropSenderOrNew(w http.ResponseWriter, r *http.Request, sh *store.Share) (string, error) {
	if id := s.dropSender(r, sh); id != "" {
		return id, nil
	}
	id, err := auth.NewID(16)
	if err != nil {
		return "", err
	}
	maxAge := int(sh.ExpiresAt - time.Now().Unix())
	if maxAge < 60 {
		maxAge = 60
	}
	http.SetCookie(w, &http.Cookie{Name: dropCookieName(sh), Value: s.dropSenderValue(sh, id), Path: "/",
		HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	return id, nil
}

// dropLocks serializes admission per link: contar, somar e inserir precisam acontecer juntos,
// senão N visitantes simultâneos leem o mesmo estado e passam todos, furando a cota.
type dropLocks struct {
	mu sync.Mutex
	m  map[int64]*sync.Mutex
}

func (d *dropLocks) lock(id int64) func() {
	d.mu.Lock()
	if d.m == nil {
		d.m = map[int64]*sync.Mutex{}
	}
	mu, ok := d.m[id]
	if !ok {
		mu = &sync.Mutex{}
		d.m[id] = mu
	}
	d.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// dropShare resolves a write request: link vivo, no modo de envio, destravado, e com um slot de
// concorrência para o IP do visitante.
func (s *Server) dropShare(w http.ResponseWriter, r *http.Request) (*vfs.Root, *store.Share, string, func(), error) {
	sh, err := s.publicShareWrite(r)
	if err != nil {
		return nil, nil, "", nil, err
	}
	// O interruptor do administrador já foi conferido em ifEnabled, na resolução do link.
	if sh.Mode != "drop" {
		return nil, nil, "", nil, errShareNotFound
	}
	if !shareUnlocked(r, sh) {
		return nil, nil, "", nil, errShareLocked
	}
	sender, err := s.dropSenderOrNew(w, r, sh)
	if err != nil {
		return nil, nil, "", nil, err
	}
	release, err := s.acquireDropSlot(r)
	if err != nil {
		return nil, nil, "", nil, err
	}
	root, err := s.base.Sub(sh.Path)
	if err != nil {
		release()
		s.log.Warn("drop folder unavailable", "id", sh.ID, "path", sh.Path, "err", err)
		return nil, nil, "", nil, errShareNotFound
	}
	// A pasta precisa continuar sendo a mesma que o dono escolheu: apagada e recriada por fora,
	// o link não volta a valer.
	if e, err := root.Stat(""); err != nil || e.Type != "dir" {
		root.Close()
		release()
		return nil, nil, "", nil, errShareNotFound
	}
	if sh.Ino != 0 {
		if dev, ino, err := root.Identity(""); err != nil || dev != sh.Dev || ino != sh.Ino {
			root.Close()
			release()
			s.log.Info("drop target replaced", "id", sh.ID, "path", sh.Path)
			return nil, nil, "", nil, errShareNotFound
		}
	}
	return root, sh, sender, func() { root.Close(); release() }, nil
}

// acquireDropSlot bounds concurrent uploads per visitor IP, waiting like public downloads do.
func (s *Server) acquireDropSlot(r *http.Request) (func(), error) {
	ip := ipFrom(r)
	ctx, cancel := context.WithTimeout(r.Context(), s.publicWait)
	defer cancel()
	if !s.dropSem.Acquire(ctx, ip) {
		return nil, errorf(http.StatusTooManyRequests, "busy", "too many concurrent uploads")
	}
	return func() { s.dropSem.Release(ip) }, nil
}

// dropOwner loads the link owner, needed to charge the upload against their disk quota.
func (s *Server) dropOwner(ctx context.Context, sh *store.Share) (*store.User, error) {
	u, err := s.db.GetUser(ctx, sh.CreatedBy)
	if err != nil {
		return nil, errShareNotFound
	}
	return u, nil
}

// admit reserves room for one incoming file on the link: quota, per-file cap and file count are
// checked and the name is resolved under the link's lock, so two visitors never both pass.
// Devolve o nome final já livre no disco.
func (s *Server) admit(ctx context.Context, root *vfs.Root, sh *store.Share, sender, name string, size int64) (string, func(), error) {
	if err := vfs.ValidName(name); err != nil {
		return "", nil, err
	}
	if sh.MaxFileBytes > 0 && size > sh.MaxFileBytes {
		return "", nil, errDropFileLimit
	}
	owner, err := s.dropOwner(ctx, sh)
	if err != nil {
		return "", nil, err
	}
	unlock := s.dropMu.lock(sh.ID)
	usage, err := s.db.ShareUsage(ctx, sh.ID)
	if err != nil {
		unlock()
		return "", nil, err
	}
	if sh.MaxFiles > 0 && usage.Count >= sh.MaxFiles {
		unlock()
		return "", nil, errDropCount
	}
	if sh.QuotaBytes > 0 && usage.Bytes+size > sh.QuotaBytes {
		unlock()
		return "", nil, errDropFull
	}
	// A cota do dono também vale: o link não pode servir de desvio para enchê-la. Ela mede o
	// que já foi finalizado, não o que está reservado — quem limita a reserva é a cota do link.
	if err := s.checkQuota(ctx, owner, size); err != nil {
		unlock()
		if isQuotaErr(err) {
			return "", nil, errDropFull // nunca contar ao visitante como anda a conta do dono
		}
		return "", nil, err
	}
	if n, err := s.db.CountOpenShareUploads(ctx, sh.ID, sender); err != nil {
		unlock()
		return "", nil, err
	} else if n >= dropOpenPerSender {
		unlock()
		return "", nil, errDropCount
	}
	final, err := s.freeName(ctx, root, sh, name)
	if err != nil {
		unlock()
		return "", nil, err
	}
	return final, unlock, nil
}

// freeName picks a name nobody is using yet in the drop folder — nem no disco, nem reservado
// por uma sessão em voo. Um visitante nunca sobrescreve o arquivo do dono nem o de outro
// remetente, e nunca fica sabendo que precisou desviar: o nome que volta para ele é o que ele
// pediu (ver `SentName`), senão o envio viraria um teste de existência de nome.
func (s *Server) freeName(ctx context.Context, root *vfs.Root, sh *store.Share, name string) (string, error) {
	open, err := s.db.ShareOpenNames(ctx, sh.ID)
	if err != nil {
		return "", err
	}
	free := func(cand string) (bool, error) {
		if open[cand] {
			return false, nil
		}
		exists, err := root.Exists(cand)
		return !exists, err
	}
	if ok, err := free(name); err != nil {
		return "", err
	} else if ok {
		return name, nil
	}
	base, ext := vfs.SplitExt(name)
	for i := 1; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if ok, err := free(cand); err != nil {
			return "", err
		} else if ok {
			return cand, nil
		}
	}
	return "", vfs.ErrExists
}

// recordDrop books a finished upload and logs who sent it. `name` é o nome no disco e `sent` o
// que o remetente pediu; só o segundo volta para ele.
func (s *Server) recordDrop(r *http.Request, sh *store.Share, sender, name, sent string, size int64) {
	if err := s.db.AddShareUpload(r.Context(), &store.ShareUpload{ShareID: sh.ID, Sender: sender, Name: name, SentName: sent, Size: size}); err != nil {
		s.log.Warn("record drop upload", "id", sh.ID, "err", err)
	}
	s.db.TouchShare(r.Context(), sh.ID)
	// O id do link vai no evento porque o segmento de token é mascarado nos logs: sem ele não
	// dá para saber qual link está sendo martelado.
	s.audit(r, nil, "share.drop.upload", map[string]any{"shareID": sh.ID, "name": name, "size": size, "sender": sender})
	s.metrics.Inc("filezam_drop_bytes_total", "", size)
	s.indexTouch(indexAncestors("", vfs.Join(sh.Path, name))...)
}

// handleDropPut receives one small file in a single request.
func (s *Server) handleDropPut(w http.ResponseWriter, r *http.Request) error {
	root, sh, sender, done, err := s.dropShare(w, r)
	if err != nil {
		return err
	}
	defer done()
	name := r.URL.Query().Get("name")
	if r.ContentLength > s.cfg.ChunkSize {
		return errorf(http.StatusRequestEntityTooLarge, "too_large", "use chunked upload for files larger than %d bytes", s.cfg.ChunkSize)
	}
	// Corpo sem Content-Length (transfer-encoding chunked): admitimos pelo pior caso e o
	// tamanho real é reconciliado depois de gravar.
	incoming := r.ContentLength
	if incoming < 0 {
		incoming = s.cfg.ChunkSize
	}
	final, unlock, err := s.admit(r.Context(), root, sh, sender, name, incoming)
	if err != nil {
		return err
	}
	// O mutex do link só é solto depois de gravar: entre reservar o nome e finalizar não pode
	// haver janela em que outro visitante pegue o mesmo nome. Isso serializa os envios pequenos
	// de um mesmo link, o que é aceitável (são no máximo chunkSize) e não afeta os grandes, que
	// soltam o mutex assim que a sessão é criada.
	mtime, _ := strconv.ParseInt(r.URL.Query().Get("mtime"), 10, 64)
	e, err := s.storeSmallFile(root, "", final, writeOpts{Mtime: mtime}, bodyReader(w, r, s.cfg.ChunkSize))
	unlock()
	if err != nil {
		return err
	}
	s.usageAdd(sh.CreatedBy, e.Size)
	s.recordDrop(r, sh, sender, final, name, e.Size)
	writeJSON(w, r, 201, map[string]any{"name": name, "size": e.Size})
	return nil
}

// handleDropUploadCreate opens a resumable session for a large file.
func (s *Server) handleDropUploadCreate(w http.ResponseWriter, r *http.Request) error {
	root, sh, sender, done, err := s.dropShare(w, r)
	if err != nil {
		return err
	}
	defer done()
	var in struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Mtime *int64 `json:"mtime"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	final, unlock, err := s.admit(r.Context(), root, sh, sender, in.Name, in.Size)
	if err != nil {
		return err
	}
	info, err := s.uploads.Create(r.Context(), root, uploads.CreateOpts{Scope: sh.Path, Name: final, SentName: in.Name, UserID: sh.CreatedBy, ShareID: sh.ID, Sender: sender, Size: in.Size, Mtime: in.Mtime})
	unlock()
	if err != nil {
		return err
	}
	s.usageAdd(sh.CreatedBy, in.Size)
	info.Name = in.Name // o remetente segue vendo o nome que pediu
	writeJSON(w, r, 201, info)
	return nil
}

// handleDropUploadChunk writes one block of an open session.
func (s *Server) handleDropUploadChunk(w http.ResponseWriter, r *http.Request) error {
	root, sh, sender, done, err := s.dropShare(w, r)
	if err != nil {
		return err
	}
	defer done()
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 0 {
		return errorf(http.StatusBadRequest, "bad_index", "index query parameter required")
	}
	if r.ContentLength < 0 {
		return errorf(http.StatusLengthRequired, "length_required", "Content-Length required")
	}
	if r.ContentLength > s.cfg.ChunkSize {
		return errorf(http.StatusRequestEntityTooLarge, "too_large", "chunk exceeds chunk size")
	}
	info, err := s.uploads.WriteChunk(r.Context(), root, uploads.SessionRef{ID: r.PathValue("id"), Scope: sh.Path, ShareID: sh.ID, Sender: sender},
		index, r.ContentLength, bodyReader(w, r, s.cfg.ChunkSize))
	if err != nil {
		return err
	}
	s.metrics.Inc("filezam_upload_bytes_total", "", r.ContentLength)
	writeJSON(w, r, 200, info)
	return nil
}

// handleDropUploadComplete finalizes a session.
func (s *Server) handleDropUploadComplete(w http.ResponseWriter, r *http.Request) error {
	root, sh, sender, done, err := s.dropShare(w, r)
	if err != nil {
		return err
	}
	defer done()
	ref := uploads.SessionRef{ID: r.PathValue("id"), Scope: sh.Path, ShareID: sh.ID, Sender: sender}
	info, err := s.uploads.Get(r.Context(), ref)
	if err != nil {
		return err
	}
	e, missing, err := s.uploads.Complete(r.Context(), root, ref)
	if err != nil {
		if errors.Is(err, uploads.ErrIncomplete) {
			ae := errorf(http.StatusConflict, "incomplete", "upload incomplete")
			ae.Extra = map[string]any{"missing": missing}
			return ae
		}
		return err
	}
	sent := info.SentName
	if sent == "" {
		sent = info.Name
	}
	s.recordDrop(r, sh, sender, info.Name, sent, e.Size)
	writeJSON(w, r, 200, map[string]any{"name": sent, "size": e.Size})
	return nil
}

// handleDropUploadAbort cancels a session and frees what it reserved.
func (s *Server) handleDropUploadAbort(w http.ResponseWriter, r *http.Request) error {
	root, sh, sender, done, err := s.dropShare(w, r)
	if err != nil {
		return err
	}
	defer done()
	if err := s.uploads.Abort(r.Context(), root, uploads.SessionRef{ID: r.PathValue("id"), Scope: sh.Path, ShareID: sh.ID, Sender: sender}); err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}

// abortShareUploads removes the open sessions of a link, parts included. Chamado quando o link
// morre (revogado, ou a pasta foi movida/apagada pelo app), para não deixar bytes reservados
// num caminho que talvez nem exista mais.
func (s *Server) abortShareUploads(ctx context.Context, shareID int64) {
	list, err := s.db.ListUploadsByShare(ctx, shareID)
	if err != nil {
		s.log.Warn("drop cleanup query", "id", shareID, "err", err)
		return
	}
	for _, u := range list {
		if err := s.base.RemovePart(u.Dir, u.ID); err != nil {
			s.log.Warn("drop cleanup: remove part", "id", u.ID, "err", err)
		}
		_ = s.db.DeleteUpload(ctx, u.ID)
	}
}

// dropInfo is what a visitor sees of an unlocked drop link.
func (s *Server) dropInfo(ctx context.Context, r *http.Request, sh *store.Share, sender string) map[string]any {
	usage, _ := s.db.ShareUsage(ctx, sh.ID)
	mine := []map[string]any{}
	if sender != "" {
		if list, err := s.db.ListShareUploads(ctx, sh.ID, sender); err == nil {
			for _, it := range list {
				mine = append(mine, map[string]any{"name": it.SentName, "size": it.Size, "at": it.CreatedAt})
			}
		}
	}
	return map[string]any{
		"quotaBytes": sh.QuotaBytes, "usedBytes": usage.Bytes, "fileCount": usage.Count,
		"maxFileBytes": sh.MaxFileBytes, "maxFiles": sh.MaxFiles,
		"chunkSize": s.cfg.ChunkSize, "maxParallel": dropClientParallel,
		"mine": mine,
	}
}

// dropClientParallel casa com o teto de uploads simultâneos por IP: mandar o cliente abrir mais
// conexões só o faria esperar por um slot.
const dropClientParallel = 2
