// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

// dropOpenPerSender limita quantas sessões em blocos um visitante mantém abertas no mesmo link.
// É repartição, não contenção: a identidade do remetente é emitida a quem pedir, então quem
// descarta o cookie volta com o contador zerado. O que segura o total é o teto de arquivos do
// link, que `ShareUsage` conta somando as sessões abertas às recebidas — esse não depende de
// cookie nenhum e é o que impede milhares de sessões de 1 byte de encherem o diretório de .part.
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

// admit checks whether one more file fits on the link: cota, teto por arquivo, contagem e
// sessões abertas. Devolve o mutex do link **ainda tomado** — quem chama decide até onde segurar:
//
//   - a sessão em blocos segura até depois de a linha existir, porque ela reserva o tamanho
//     declarado no disco na hora (fallocate) e soltar antes deixaria duas sessões simultâneas
//     passarem pela mesma leitura de cota e reservarem o dobro;
//   - o envio único solta na hora, porque o que vem depois é a transferência, que dura o tempo
//     da rede de quem envia. Ali a cota pode ser ultrapassada pelo que está em voo, limitado aos
//     envios simultâneos por IP vezes o tamanho de um bloco.
//
// O que o mutex nunca cobre, em nenhum dos dois caminhos, é a transferência dos bytes.
func (s *Server) admit(ctx context.Context, sh *store.Share, sender, name string, size int64) (func(), error) {
	if err := vfs.ValidName(name); err != nil {
		return nil, err
	}
	if sh.MaxFileBytes > 0 && size > sh.MaxFileBytes {
		return nil, errDropFileLimit
	}
	owner, err := s.dropOwner(ctx, sh)
	if err != nil {
		return nil, err
	}
	unlock := s.dropMu.lock(sh.ID)
	fail := func(err error) (func(), error) {
		unlock()
		return nil, err
	}
	usage, err := s.db.ShareUsage(ctx, sh.ID)
	if err != nil {
		return fail(err)
	}
	if sh.MaxFiles > 0 && usage.Count >= sh.MaxFiles {
		return fail(errDropCount)
	}
	if sh.QuotaBytes > 0 && usage.Bytes+size > sh.QuotaBytes {
		return fail(errDropFull)
	}
	// A cota do dono também vale: o link não pode servir de desvio para enchê-la. Ela mede o
	// que já foi finalizado, não o que está reservado — quem limita a reserva é a cota do link.
	if err := s.checkQuota(ctx, owner, size); err != nil {
		if isQuotaErr(err) {
			return fail(errDropFull) // nunca contar ao visitante como anda a conta do dono
		}
		return fail(err)
	}
	if n, err := s.db.CountOpenShareUploads(ctx, sh.ID, sender); err != nil {
		return fail(err)
	} else if n >= dropOpenPerSender {
		return fail(errDropCount)
	}
	return unlock, nil
}

// publish gives the part file its final name. Roda sob o mutex do link e só faz metadados, então
// não segura ninguém: a colisão com um nome que apareceu no meio do caminho é resolvida aqui,
// tentando o próximo nome livre.
func (s *Server) publish(ctx context.Context, root *vfs.Root, sh *store.Share, id, want string) (string, error) {
	unlock := s.dropMu.lock(sh.ID)
	defer unlock()
	name := want
	for i := 0; i < 5; i++ {
		free, err := s.freeName(ctx, root, sh, name)
		if err != nil {
			return "", err
		}
		err = root.Finalize("", id, free, false)
		if err == nil {
			return free, nil
		}
		if !errors.Is(err, vfs.ErrExists) {
			return "", err
		}
		name = free
	}
	return "", vfs.ErrExists
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
	// Contexto sem cancelamento: o arquivo já está publicado no disco. Se o recibo dependesse da
	// conexão de quem enviou, um fechamento de aba entre a publicação e o INSERT deixaria o
	// arquivo lá e fora da cota — invisível para o teto do link e para a lista do remetente,
	// para sempre.
	ctx := context.WithoutCancel(r.Context())
	if err := s.db.AddShareUpload(ctx, &store.ShareUpload{ShareID: sh.ID, Sender: sender, Name: name, SentName: sent, Size: size}); err != nil {
		s.log.Warn("record drop upload", "id", sh.ID, "err", err)
	}
	s.db.TouchShare(ctx, sh.ID)
	s.notifyDrop(ctx, sh, name, size)
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
	unlock, err := s.admit(r.Context(), sh, sender, name, incoming)
	if err != nil {
		return err
	}
	unlock() // o que vem agora é a transferência: nunca sob o mutex
	mtime, _ := strconv.ParseInt(r.URL.Query().Get("mtime"), 10, 64)
	final, size, err := s.storeDropFile(r.Context(), root, sh, name, mtime, bodyReader(w, r, s.cfg.ChunkSize))
	if err != nil {
		return err
	}
	s.usageAdd(sh.CreatedBy, size)
	s.recordDrop(r, sh, sender, final, name, size)
	writeJSON(w, r, 201, map[string]any{"name": name, "size": size})
	return nil
}

// storeDropFile receives one file and publishes it under a free name.
//
// A transferência acontece **fora** do mutex do link: ela dura o tempo da rede do visitante, e
// segurar o mutex ali fazia o segundo envio do mesmo link esperar a subida inteira do primeiro —
// o suficiente para estourar o tempo limite de um proxy e virar erro na tela de quem enviou.
// O nome só é decidido na publicação, que é uma operação de metadados.
func (s *Server) storeDropFile(ctx context.Context, root *vfs.Root, sh *store.Share, want string, mtime int64, body io.Reader) (string, int64, error) {
	id, err := auth.NewID(8)
	if err != nil {
		return "", 0, err
	}
	f, err := root.CreatePart("", id, 0)
	if err != nil {
		return "", 0, err
	}
	fail := func(err error) (string, int64, error) {
		f.Close()
		_ = root.RemovePart("", id)
		return "", 0, vfs.MapError(err)
	}
	n, err := io.Copy(f, body)
	if err != nil {
		return fail(err)
	}
	if s.cfg.Fsync {
		if err := f.Sync(); err != nil {
			return fail(err)
		}
	}
	if err := f.Close(); err != nil {
		_ = root.RemovePart("", id)
		return "", 0, vfs.MapError(err)
	}
	if mtime > 0 {
		_ = root.Chtimes(vfs.Join("", vfs.PartName(id)), uploads.ClampMtime(mtime))
	}
	final, err := s.publish(ctx, root, sh, id, want)
	if err != nil {
		_ = root.RemovePart("", id)
		return "", 0, err
	}
	return final, n, nil
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
	// O mutex vem tomado do admit e só é solto depois de a sessão existir: ela reserva o tamanho
	// declarado no disco, então conferir a cota e inserir precisam ser uma coisa só. Tudo aqui é
	// metadado; os blocos chegam depois, sem segurar ninguém.
	unlock, err := s.admit(r.Context(), sh, sender, in.Name, in.Size)
	if err != nil {
		return err
	}
	final, err := s.freeName(r.Context(), root, sh, in.Name)
	if err != nil {
		unlock()
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
