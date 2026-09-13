package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/miguelzamberlan/filezam/internal/jobs"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// archiveExt reconhece o que dá para extrair. Só .zip por enquanto: o tar traz uma superfície
// própria (entradas esparsas, hardlinks, e o descarte por descompressão de entradas puladas) que
// merece o seu próprio trabalho.
// extractGlobalMax limita as extrações simultâneas no servidor inteiro: cada uma carrega o
// diretório central do zip na memória, então o teto por usuário não basta.
const extractGlobalMax = 2

func archiveExt(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".zip") }

// extractDirName deriva o nome da pasta de destino do nome do arquivo. Cai no nome inteiro quando
// o que sobra não serve (".zip" sozinho, por exemplo).
func extractDirName(name string) string {
	base := name
	if archiveExt(base) {
		base = base[:len(base)-4]
	}
	if vfs.ValidName(base) != nil {
		base = name
	}
	return base
}

// handleExtract unpacks an archive into a new folder beside it.
func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	set := s.settings()
	if err := requireFeature(set.ExtractEnabled); err != nil {
		return err
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := vfs.NormalizeWritable(in.Path)
	if err != nil {
		return err
	}
	if p == "" {
		return vfs.ErrRootOp
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	if e.Type == "dir" {
		return vfs.ErrIsDir
	}
	if e.Type != "file" || !archiveExt(e.Name) {
		return errBadArchive
	}
	// O tamanho do arquivo é o único teto que vale antes de o zip carregar o diretório central.
	if e.Size > set.ExtractMaxArchive {
		return errorf(http.StatusRequestEntityTooLarge, "archive_too_large", "archive above the %d byte limit", set.ExtractMaxArchive)
	}
	// Quem já está com a cota estourada não começa; o teto real é conferido durante a escrita.
	if err := s.checkQuota(r.Context(), u, 0); err != nil {
		return err
	}
	key := strconv.FormatInt(u.ID, 10)
	if !s.extractSem.TryAcquire(key) {
		return errorf(http.StatusTooManyRequests, "busy", "another extraction is already running")
	}
	if !s.archiveSem.TryAcquire() {
		s.extractSem.Release(key)
		return errorf(http.StatusTooManyRequests, "busy", "too many extractions on the server; try again")
	}
	release := func() { s.archiveSem.Release(); s.extractSem.Release(key) }
	// Zip corrompido ou protegido por senha é recusado na hora, com código próprio, em vez de
	// virar um job falhado (ou uma pasta só com subpastas vazias). Dentro dos semáforos, porque
	// abrir o zip carrega o diretório central na memória.
	lim := vfs.ExtractLimits{MaxBytes: set.ExtractMaxBytes, MaxEntries: int(set.ExtractMaxEntries)}
	if err := root.CheckZip(p, lim); err != nil {
		release()
		return err
	}
	parent := vfs.Dir(p)
	j, err := s.startJob(u, "extract", jobLabel([]string{p}, parent), parentDirs(nil, parent), func(ctx context.Context, j *jobs.Job) error {
		// Os semáforos são soltos aqui dentro: o job sobrevive à requisição que o criou.
		defer release()
		defer s.unguardJob(j)
		root, err := s.scopeRoot(u)
		if err != nil {
			return err
		}
		defer root.Close()
		dst, err := s.newExtractDir(root, parent, extractDirName(vfs.Base(p)))
		if err != nil {
			return err
		}
		s.guardJob(j, vfs.Join(u.Scope, dst), cleanupTree)
		res, err := root.ExtractZip(ctx, p, dst, lim, progressFor(j))
		if err != nil {
			// A pasta é nossa e nasceu vazia: some inteira. Remove, não RemoveTree, porque no
			// cancelamento o contexto já está morto e a varredura não apagaria nada.
			_ = root.Remove(dst)
			if errors.Is(err, vfs.ErrArchiveLimit) {
				return errors.New("archive too large to extract")
			}
			return err
		}
		s.usageAdd(u.ID, res.Bytes)
		s.indexTree(vfs.Join(u.Scope, dst))
		j.AddDir(parent)
		switch {
		case res.Encrypted > 0:
			j.Warn(strconv.Itoa(res.Skipped) + " entries skipped (" + strconv.Itoa(res.Encrypted) + " password-protected)")
		case res.Skipped > 0:
			j.Warn(strconv.Itoa(res.Skipped) + " entries skipped")
		}
		return nil
	})
	if err != nil {
		release()
		return err
	}
	return respondJob(w, r, j)
}

// newExtractDir creates the destination folder, stepping aside if the name is taken. O Mkdir pode
// perder a corrida para outro escritor (Samba), então tenta de novo com um nome livre.
func (s *Server) newExtractDir(root *vfs.Root, parent, name string) (string, error) {
	for i := 0; i < 4; i++ {
		dst := vfs.Join(parent, name)
		err := root.Mkdir(dst)
		if err == nil {
			return dst, nil
		}
		if !errors.Is(err, vfs.ErrExists) {
			return "", err
		}
		if name, err = root.UniqueName(parent, name); err != nil {
			return "", err
		}
	}
	return "", vfs.ErrExists
}

// handleArchive zips the given paths into a new .zip beside them.
func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := requireFeature(s.settings().ExtractEnabled); err != nil {
		return err
	}
	var in struct {
		Paths []string `json:"paths"`
		Name  string   `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	paths, err := normalizeWritableList(in.Paths)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if _, err := root.Stat(p); err != nil {
			return err
		}
	}
	parent := vfs.Dir(paths[0])
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = vfs.Base(paths[0])
	}
	name = strings.TrimSuffix(name, ".zip") + ".zip"
	if err := vfs.ValidName(name); err != nil {
		return err
	}
	key := strconv.FormatInt(u.ID, 10)
	if !s.extractSem.TryAcquire(key) {
		return errorf(http.StatusTooManyRequests, "busy", "another archive operation is already running")
	}
	release := func() { s.extractSem.Release(key) }

	j, err := s.startJob(u, "archive", jobLabel(paths, parent), parentDirs(nil, parent), func(ctx context.Context, j *jobs.Job) error {
		defer release()
		defer s.unguardJob(j)
		root, err := s.scopeRoot(u)
		if err != nil {
			return err
		}
		defer root.Close()
		var total vfs.Totals
		for _, p := range paths {
			t, err := root.Scan(ctx, p)
			if err != nil {
				return err
			}
			total.Files += t.Files
			total.Bytes += t.Bytes
		}
		if err := s.checkQuota(ctx, u, total.Bytes); err != nil {
			return errors.New(toAPIError(err).Message)
		}
		j.SetTotals(total.Files, total.Bytes)
		final, err := root.UniqueIfExists(parent, name)
		if err != nil {
			return err
		}
		s.guardJob(j, vfs.Join(u.Scope, vfs.Join(parent, vfs.ZipTempName(final))), cleanupTree)
		if err := root.WriteZipFile(ctx, parent, final, paths, progressFor(j)); err != nil {
			return err
		}
		s.usageAdd(u.ID, total.Bytes)
		s.indexTouch(indexAncestors(u.Scope, vfs.Join(u.Scope, vfs.Join(parent, final)))...)
		j.AddDir(parent)
		return nil
	})
	if err != nil {
		release()
		return err
	}
	return respondJob(w, r, j)
}
