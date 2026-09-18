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
	"encoding/csv"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/miguelzamberlan/filezam/internal/jobs"
	"github.com/miguelzamberlan/filezam/internal/media"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// mediaSyncMax é quantos cabeçalhos ainda não lidos a requisição lê na hora. Acima disso a
// leitura vira job: abrir dez mil arquivos num disco externo leva minutos, e o navegador não
// pode ficar segurando uma conexão por isso. Como toda leitura fica em cache, a mesma análise
// pedida depois que o job termina volta na hora.
const mediaSyncMax = 250

// mediaListMax é até quantos arquivos a resposta detalha um a um. Acima disso só os totais:
// uma pasta com cem mil vídeos viraria uma resposta de dezenas de megabytes.
const mediaListMax = 2000

// mediaSyncTimeout limita a leitura feita dentro da requisição.
const mediaSyncTimeout = 25 * time.Second

// mediaPathsMax é quantos itens uma seleção pode trazer.
const mediaPathsMax = 1000

// candidate is one file the analysis will account for.
type candidate struct {
	path  string // relativo ao root do usuário
	name  string
	size  int64
	mtime int64
	media bool // a extensão promete foto ou vídeo
}

// mediaFile is one line of the per-file listing.
type mediaFile struct {
	Path string      `json:"path"`
	Name string      `json:"name"`
	Size int64       `json:"size"`
	Meta *media.Meta `json:"meta,omitempty"`
}

// handleMediaStats analyses one or more paths: cada pasta é percorrida inteira, cada arquivo
// entra sozinho. Devolve os totais e, quando são poucos, a lista arquivo a arquivo.
func (s *Server) handleMediaStats(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	set := s.settings()
	if err := requireFeature(set.MediaEnabled); err != nil {
		return err
	}
	var in struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if len(in.Paths) == 0 || len(in.Paths) > mediaPathsMax {
		return errorf(http.StatusBadRequest, "bad_paths", "between 1 and %d paths are required", mediaPathsMax)
	}
	paths := make([]string, 0, len(in.Paths))
	for _, p := range in.Paths {
		// NormalizeWritable, e não Normalize: nomes reservados (.filezam-*) não são
		// endereçáveis nem para leitura, aqui como em toda rota de arquivo.
		np, err := vfs.NormalizeWritable(p)
		if err != nil {
			return err
		}
		paths = append(paths, np)
	}

	cands, dirs, partial, err := collectMedia(r.Context(), root, paths, int(set.MediaMaxScan))
	if err != nil {
		return err
	}
	cached, misses, err := s.mediaCached(r.Context(), u, cands)
	if err != nil {
		return err
	}
	// Muita leitura nova: vira job, e o cliente pede de novo quando ele terminar.
	if len(misses) > mediaSyncMax {
		j, err := s.startMediaJob(u, paths, misses)
		if err != nil {
			return err
		}
		writeJSON(w, r, 202, map[string]any{"job": j.Snapshot(), "pending": len(misses)})
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), mediaSyncTimeout)
	defer cancel()
	read, err := s.probeAll(ctx, root, u, misses)
	if err != nil {
		return err
	}
	for p, m := range read {
		cached[p] = m
	}

	agg := media.NewAggregator()
	for i := 0; i < dirs; i++ {
		agg.AddDir()
	}
	if partial {
		agg.SetPartial()
	}
	files := make([]mediaFile, 0, min(len(cands), mediaListMax))
	for _, c := range cands {
		m := cached[c.path]
		agg.Add(media.Sample{Path: c.path, Name: c.name, Size: c.size, Meta: m, Media: c.media})
		if len(cands) <= mediaListMax {
			files = append(files, mediaFile{Path: c.path, Name: c.name, Size: c.size, Meta: m})
		}
	}
	out := map[string]any{"stats": agg.Stats()}
	if len(cands) <= mediaListMax {
		out["files"] = files
	}
	writeJSON(w, r, 200, out)
	return nil
}

// handleMediaCSV streams the per-file readings as a spreadsheet. Só o que já está em cache é
// lido de novo do disco; o resto é lido na hora, dentro do mesmo teto da análise síncrona.
func (s *Server) handleMediaCSV(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	set := s.settings()
	if err := requireFeature(set.MediaEnabled); err != nil {
		return err
	}
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	cands, _, partial, err := collectMedia(r.Context(), root, []string{p}, int(set.MediaMaxScan))
	if err != nil {
		return err
	}
	cached, misses, err := s.mediaCached(r.Context(), u, cands)
	if err != nil {
		return err
	}
	if len(misses) > mediaSyncMax {
		return errorf(http.StatusConflict, "media_pending", "run the analysis first: %d files still unread", len(misses))
	}
	ctx, cancel := context.WithTimeout(r.Context(), mediaSyncTimeout)
	defer cancel()
	read, err := s.probeAll(ctx, root, u, misses)
	if err != nil {
		return err
	}
	for k, v := range read {
		cached[k] = v
	}

	name := vfs.Base(p)
	if name == "" {
		name = "filezam"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + "-midia.csv"}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"caminho", "nome", "bytes", "tipo", "formato", "largura", "altura",
		"orientacao", "duracao_ms", "codec", "codec_audio", "fps", "bitrate", "camera", "lente",
		"iso", "obturador", "abertura", "distancia_focal", "data"})
	for _, c := range cands {
		m := cached[c.path]
		row := []string{"/" + c.path, c.name, strconv.FormatInt(c.size, 10)}
		if m == nil {
			row = append(row, make([]string, 17)...)
		} else {
			taken := ""
			if m.TakenAt > 0 {
				taken = time.UnixMilli(m.TakenAt).UTC().Format("2006-01-02 15:04:05")
			}
			row = append(row,
				m.Kind, m.Format, itoa(m.Width), itoa(m.Height), m.Orientation(),
				i64toa(m.DurationMS), m.Codec, m.AudioCodec, media.FPSKey(m.FPSMilli),
				i64toa(m.Bitrate), m.Camera, m.Lens, itoa(m.ISO), m.Exposure,
				ftoa(m.FNumber), ftoa(m.FocalLength), taken)
		}
		if err := cw.Write(row); err != nil {
			return nil // cliente desistiu do download
		}
	}
	if partial {
		_ = cw.Write([]string{"# análise parcial: o limite de varredura foi atingido"})
	}
	return nil
}

func itoa(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func i64toa(v int64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

func ftoa(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// collectMedia walks the requested paths and lists every regular file below them, without
// reading a single byte of content. Pastas repetidas na seleção contam uma vez só.
func collectMedia(ctx context.Context, root *vfs.Root, paths []string, maxScan int) (cands []candidate, dirs int, partial bool, err error) {
	seenFiles := map[string]bool{}
	seenDirs := map[string]bool{}
	scanned := 0
	addFile := func(p string, e vfs.Entry) {
		if seenFiles[p] {
			return
		}
		seenFiles[p] = true
		cands = append(cands, candidate{path: p, name: e.Name, size: e.Size, mtime: e.Mtime, media: media.Supported(e.Name)})
	}
	for _, p := range paths {
		e, err := root.Stat(p)
		if err != nil {
			return nil, 0, false, err
		}
		if e.Type != "dir" {
			if e.Type == "file" {
				addFile(p, *e)
			}
			continue
		}
		werr := root.WalkEntries(ctx, p, func(cp string, ce vfs.Entry) error {
			scanned++
			if scanned > maxScan {
				return vfs.ErrScanLimit
			}
			switch ce.Type {
			case "dir":
				if cp != p && !seenDirs[cp] {
					seenDirs[cp] = true
					dirs++
				}
			case "file":
				addFile(cp, ce)
			}
			return nil
		})
		switch {
		case errors.Is(werr, vfs.ErrScanLimit):
			return cands, dirs, true, nil
		case werr != nil:
			return nil, 0, false, werr
		}
	}
	return cands, dirs, partial, nil
}

// mediaCached splits the candidates into what the cache already knows and what still has to be
// read from disk. Só arquivos de extensão conhecida são lidos: o resto conta como "outros".
func (s *Server) mediaCached(ctx context.Context, u *store.User, cands []candidate) (map[string]*media.Meta, []candidate, error) {
	want := make(map[string][2]int64, len(cands))
	for _, c := range cands {
		if c.media {
			want[vfs.Join(u.Scope, c.path)] = [2]int64{c.size, c.mtime}
		}
	}
	rows, err := s.db.GetMediaMeta(ctx, want)
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]*media.Meta, len(rows))
	var misses []candidate
	for _, c := range cands {
		if !c.media {
			continue
		}
		row, ok := rows[vfs.Join(u.Scope, c.path)]
		if !ok {
			misses = append(misses, c)
			continue
		}
		if !row.Readable {
			continue // cache negativo: o cabeçalho já foi tentado e não abriu
		}
		var m media.Meta
		if err := json.Unmarshal([]byte(row.Meta), &m); err != nil {
			misses = append(misses, c)
			continue
		}
		out[c.path] = &m
	}
	return out, misses, nil
}

// probeAll reads the headers of the given files and stores what it found, including the
// failures: um arquivo que não abre uma vez não precisa ser reaberto na próxima análise.
func (s *Server) probeAll(ctx context.Context, root *vfs.Root, u *store.User, list []candidate) (map[string]*media.Meta, error) {
	out := make(map[string]*media.Meta, len(list))
	rows := make([]store.MediaRow, 0, len(list))
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		// O contexto da requisição pode já ter morrido; o cache é melhor esforço e a gravação
		// usa o contexto de fundo para não perder o que acabou de ser lido.
		if err := s.db.PutMediaMeta(s.bg, rows); err != nil {
			s.log.Warn("media cache write", "err", err)
		}
		rows = rows[:0]
		return nil
	}
	for _, c := range list {
		if err := ctx.Err(); err != nil {
			break // o que já foi lido continua valendo, e o resto fica para a próxima
		}
		m, ok := probeOne(root, c)
		row := store.MediaRow{Path: vfs.Join(u.Scope, c.path), Size: c.size, Mtime: c.mtime, Readable: ok}
		if ok {
			out[c.path] = m
			if b, err := json.Marshal(m); err == nil {
				row.Meta = string(b)
			}
		}
		rows = append(rows, row)
		if len(rows) >= 200 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// probeOne reads a single header through the sandbox, never touching the host path directly.
func probeOne(root *vfs.Root, c candidate) (*media.Meta, bool) {
	f, fi, err := root.OpenFile(c.path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	m, err := media.Probe(f, c.name, fi.Size())
	if err != nil {
		return nil, false
	}
	return &m, true
}

// startMediaJob reads the pending headers in the background, filling the cache. Não escreve
// nada na árvore do usuário, então não tem o que desfazer se o processo cair no meio.
func (s *Server) startMediaJob(u *store.User, paths []string, misses []candidate) (*jobs.Job, error) {
	return s.startJob(u, "media", jobLabel(paths, ""), nil, func(ctx context.Context, j *jobs.Job) error {
		root, err := s.scopeRoot(u)
		if err != nil {
			return err
		}
		defer root.Close()
		j.SetTotals(len(misses), 0)
		rows := make([]store.MediaRow, 0, 200)
		flush := func() {
			if len(rows) == 0 {
				return
			}
			if err := s.db.PutMediaMeta(s.bg, rows); err != nil {
				s.log.Warn("media cache write", "err", err)
			}
			rows = rows[:0]
		}
		failed := 0
		for _, c := range misses {
			if err := ctx.Err(); err != nil {
				flush()
				return err
			}
			j.SetCurrent(c.name)
			m, ok := probeOne(root, c)
			row := store.MediaRow{Path: vfs.Join(u.Scope, c.path), Size: c.size, Mtime: c.mtime, Readable: ok}
			if ok {
				if b, err := json.Marshal(m); err == nil {
					row.Meta = string(b)
				}
			} else {
				failed++
			}
			rows = append(rows, row)
			if len(rows) >= 200 {
				flush()
			}
			j.Add(1, c.size)
		}
		flush()
		if failed > 0 {
			j.Warn(strconv.Itoa(failed) + " files could not be read")
		}
		return nil
	})
}

// mediaOf reads one file's metadata for the details dialog, preferring the cache.
func (s *Server) mediaOf(ctx context.Context, root *vfs.Root, u *store.User, p string, e *vfs.Entry) *media.Meta {
	if !media.Supported(e.Name) {
		return nil
	}
	c := candidate{path: p, name: e.Name, size: e.Size, mtime: e.Mtime, media: true}
	cached, misses, err := s.mediaCached(ctx, u, []candidate{c})
	if err != nil {
		return nil
	}
	if m := cached[p]; m != nil {
		return m
	}
	if len(misses) == 0 {
		return nil
	}
	read, err := s.probeAll(ctx, root, u, misses)
	if err != nil {
		return nil
	}
	return read[p]
}
