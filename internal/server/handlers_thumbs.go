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
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/miguelzamberlan/filezam/internal/thumbs"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// thumbMaxAge é curto de propósito. A miniatura é a única resposta de conteúdo do produto que
// não é `no-store`: sem nenhum cache, rolar uma pasta de fotos para baixo e voltar refaria uma
// requisição por imagem. Mas é conteúdo do usuário, e o que o navegador guarda em disco continua
// lá depois do logout — então a janela de reuso livre cobre a navegação (rolar, entrar numa pasta
// e voltar) e não a sessão inteira.
//
// Passados os cinco minutos a entrada não é descartada: o ETag a revalida com um 304 vazio, que
// custa um ida-e-volta e nenhum byte de imagem. Sem `immutable`, justamente para que essa
// revalidação aconteça.
const thumbMaxAge = 300

// thumbWait: quanto uma requisição espera por um slot de geração antes de desistir e deixar o
// cliente com o ícone.
const thumbWait = 20 * time.Second

// errNoThumb: não há miniatura para este arquivo (formato sem decodificador, imagem grande demais,
// recurso desligado). O cliente cai no ícone normal, então isso não é erro para o usuário.
var errNoThumb = errorf(http.StatusNotFound, "no_thumb", "no thumbnail for this file")

// handleThumb serves a cached thumbnail, generating it on first use.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	set := s.settings()
	if !set.ThumbsEnabled || s.thumbs == nil {
		return errNoThumb
	}
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	if p == "" {
		return vfs.ErrRootOp
	}
	if !thumbs.Supported(p) {
		return errNoThumb
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	if e.Type != "file" {
		return errNoThumb
	}
	// Gerar custa CPU, então há um teto por usuário — mas ele **espera** por um slot em vez de
	// recusar. O navegador pede a pasta inteira de uma vez: um 429 viraria ícone permanente na
	// tela, porque um <img> que falha não tenta de novo. É a mesma razão pela qual o download
	// público espera (ver acquirePublicDL).
	key := strconv.FormatInt(u.ID, 10)
	ctx, cancel := context.WithTimeout(r.Context(), thumbWait)
	defer cancel()
	if !s.thumbSem.Acquire(ctx, key) {
		return errNoThumb
	}
	defer s.thumbSem.Release(key)

	lim := thumbs.Limits{MaxPixels: set.ThumbsMaxPixels, MaxFile: set.ThumbsMaxFile}
	path, err := s.thumbs.Get(r.Context(), root, p, e.Mtime, e.Size, lim)
	if err != nil {
		// Imagem que não dá para decodificar, ou grande demais, não é erro do usuário: o cliente
		// simplesmente volta para o ícone.
		if errors.Is(err, thumbs.ErrUnsupported) || errors.Is(err, thumbs.ErrTooLarge) {
			return errNoThumb
		}
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return errNoThumb
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return errNoThumb
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "private, max-age="+strconv.Itoa(thumbMaxAge))
	h.Set("ETag", thumbs.ETag(path))
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", fi.ModTime(), f)
	return nil
}
