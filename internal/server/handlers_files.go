package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/jobs"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/uploads"
	"github.com/zamberlan/filezam/internal/vfs"
)

// userRoot opens the scope root of the requesting user.
func (s *Server) userRoot(r *http.Request) (*vfs.Root, *store.User, error) {
	u := userFrom(r)
	root, err := s.scopeRoot(u)
	if err != nil {
		return nil, nil, err
	}
	return root, u, nil
}

// queryPath normaliza ?path= para leitura. Nomes reservados (.filezam-*) são
// recusados também aqui: partes de upload em andamento nunca são endereçáveis.
func queryPath(r *http.Request, key string) (string, error) {
	return vfs.NormalizeWritable(r.URL.Query().Get(key))
}

func queryBool(r *http.Request, key string) bool {
	v := r.URL.Query().Get(key)
	return v == "1" || v == "true"
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) error {
	root, _, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	l, err := root.List(p)
	if err != nil {
		return err
	}
	if len(l.Parts) > 0 {
		s.uploads.PruneOrphans(r.Context(), root, p, l.Parts)
	}
	q := r.URL.Query()
	if q.Get("limit") == "" {
		writeJSON(w, r, 200, map[string]any{"path": p, "entries": l.Entries})
		return nil
	}
	// Paginado: o servidor filtra ocultos, ordena como a interface e devolve uma fatia,
	// para pastas enormes não gerarem um JSON de dezenas de MB de uma vez.
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > listPageMax {
		limit = listPageMax
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	entries := l.Entries
	hidden := 0
	if q.Get("hidden") != "1" {
		kept := entries[:0]
		for _, e := range entries {
			if strings.HasPrefix(e.Name, ".") {
				hidden++
				continue
			}
			kept = append(kept, e)
		}
		entries = kept
	}
	key := q.Get("sort")
	if key != "size" && key != "mtime" && key != "type" {
		key = "name"
	}
	vfs.SortEntries(entries, key, q.Get("dir") == "desc")
	total := len(entries)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, r, 200, map[string]any{"path": p, "entries": entries[offset:end], "total": total, "offset": offset, "hidden": hidden})
	return nil
}

// listPageMax bounds one page of a paginated listing.
const listPageMax = 5000

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) error {
	root, _, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"path": p, "entry": e})
	return nil
}

// handleDisk reports the space of the filesystem backing the user's scope.
func (s *Server) handleDisk(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	d := root.Disk()
	out := map[string]any{"total": d.Total, "free": d.Free, "used": d.Total - d.Free}
	for k, v := range s.quotaView(r.Context(), u) {
		out[k] = v
	}
	writeJSON(w, r, 200, out)
	return nil
}

const infoScanLimit = 200000

// handleInfo returns properties of an entry: totals for folders, its shares and favorite state.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	out := map[string]any{"path": p, "entry": e}
	if e.Type == "dir" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		t, serr := root.ScanLimited(ctx, p, infoScanLimit)
		cancel()
		partial := serr != nil
		out["totals"] = map[string]any{"files": t.Files, "dirs": t.Dirs, "bytes": t.Bytes, "partial": partial}
		uid := u.ID
		if u.IsAdmin() {
			uid = 0
		}
		shares, err := s.db.ListSharesByPath(r.Context(), vfs.Join(u.Scope, p), uid)
		if err != nil {
			return err
		}
		views := make([]shareView, 0, len(shares))
		for _, sh := range shares {
			views = append(views, s.viewShare(sh, u))
		}
		out["shares"] = views
		favs, err := s.db.ListFavorites(r.Context(), u.ID)
		if err != nil {
			return err
		}
		fav := false
		for _, f := range favs {
			if f.Path == vfs.Join(u.Scope, p) {
				fav = true
				break
			}
		}
		out["favorite"] = fav
	}
	writeJSON(w, r, 200, out)
	return nil
}

func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) error {
	root, _, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	return serveFile(w, r, root, p, queryBool(r, "inline"))
}

var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
	"image/bmp": true, "image/svg+xml": true, "image/x-icon": true, "application/pdf": true,
}

var textExt = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".log": true, ".json": true, ".xml": true, ".yml": true, ".yaml": true,
	".csv": true, ".ini": true, ".conf": true, ".cfg": true, ".toml": true, ".sh": true, ".py": true, ".go": true,
	".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".css": true, ".html": true, ".htm": true, ".sql": true,
	".env": true, ".gitignore": true, ".java": true, ".c": true, ".h": true, ".cpp": true, ".rs": true, ".rb": true,
	".php": true, ".bat": true, ".ps1": true, ".srt": true, ".vtt": true,
}

// detectType returns the content type and whether inline display is permitted.
func detectType(name string, f io.ReadSeeker) (ctype string, inlineOK bool) {
	ext := strings.ToLower(path.Ext(name))
	ctype = mime.TypeByExtension(ext)
	if ctype == "" {
		buf := make([]byte, 512)
		n, _ := io.ReadFull(f, buf)
		f.Seek(0, io.SeekStart)
		ctype = http.DetectContentType(buf[:n])
	}
	base := ctype
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	switch {
	case inlineTypes[base], strings.HasPrefix(base, "video/"), strings.HasPrefix(base, "audio/"):
		return base, true
	case textExt[ext], strings.HasPrefix(base, "text/"), base == "application/json", base == "application/xml", base == "application/javascript":
		return "text/plain; charset=utf-8", true
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return ctype, false
}

// serveFile streams a file with range support, as attachment or (when safe) inline.
func serveFile(w http.ResponseWriter, r *http.Request, root *vfs.Root, p string, inline bool) error {
	if p == "" {
		return vfs.ErrIsDir
	}
	f, fi, err := root.OpenFile(p)
	if err != nil {
		return err
	}
	defer f.Close()
	name := vfs.Base(p)
	ctype, inlineOK := detectType(name, f)
	h := w.Header()
	h.Set("Cache-Control", "private, no-store")
	if inline && inlineOK {
		h.Set("Content-Type", ctype)
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
		// frame-ancestors 'self' + SAMEORIGIN: o preview de PDF é um iframe da própria SPA;
		// o DENY global do middleware bloquearia até o mesmo origin.
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'")
		h.Set("X-Frame-Options", "SAMEORIGIN")
	} else {
		if inlineOK {
			h.Set("Content-Type", ctype)
		} else {
			h.Set("Content-Type", "application/octet-stream")
		}
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
	return nil
}

// serveZip streams a zip of paths.
func serveZip(w http.ResponseWriter, r *http.Request, root *vfs.Root, paths []string, name string) error {
	if len(paths) == 0 {
		return errorf(http.StatusBadRequest, "no_paths", "no paths given")
	}
	for _, p := range paths {
		if _, err := root.Stat(p); err != nil {
			return err
		}
	}
	if name == "" {
		if len(paths) == 1 && paths[0] != "" {
			name = vfs.Base(paths[0])
		} else {
			name = "filezam-" + time.Now().Format("20060102-150405")
		}
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	w.WriteHeader(200)
	if err := root.WriteZip(r.Context(), w, paths); err != nil {
		// headers already sent; abort the connection so the client sees a failure
		panic(http.ErrAbortHandler)
	}
	return nil
}

func (s *Server) handleZip(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	zipKey := strconv.FormatInt(u.ID, 10)
	if !s.zipSem.TryAcquire(zipKey) {
		return errorf(http.StatusTooManyRequests, "busy", "too many concurrent zips")
	}
	defer s.zipSem.Release(zipKey)
	var paths []string
	for _, raw := range r.URL.Query()["path"] {
		p, err := vfs.Normalize(raw)
		if err != nil {
			return err
		}
		paths = append(paths, p)
	}
	name := r.URL.Query().Get("name")
	if name != "" && vfs.ValidName(name) != nil {
		name = ""
	}
	return serveZip(w, r, root, paths, name)
}

// deadlineReader extends the read deadline while data keeps flowing.
type deadlineReader struct {
	r    io.Reader
	rc   *http.ResponseController
	n    int64
	idle time.Duration
}

func (d *deadlineReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.n += int64(n)
	if d.n >= 1<<20 {
		d.n = 0
		_ = d.rc.SetReadDeadline(time.Now().Add(d.idle))
	}
	return n, err
}

func bodyReader(w http.ResponseWriter, r *http.Request, limit int64) io.Reader {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(60 * time.Second))
	return &deadlineReader{r: http.MaxBytesReader(w, r.Body, limit), rc: rc, idle: 60 * time.Second}
}

// storeSmallFile writes body to dir/name via a temp part and finalizes it.
func (s *Server) storeSmallFile(root *vfs.Root, dir, name string, mtime int64, overwrite bool, body io.Reader) (*vfs.Entry, error) {
	if err := vfs.ValidName(name); err != nil {
		return nil, err
	}
	if err := root.MkdirAll(dir); err != nil {
		return nil, err
	}
	if !overwrite {
		if ok, err := root.Exists(vfs.Join(dir, name)); err != nil {
			return nil, err
		} else if ok {
			return nil, errors.Join(vfs.ErrExists, errors.New(name))
		}
	}
	id, err := auth.NewID(8)
	if err != nil {
		return nil, err
	}
	f, err := root.CreatePart(dir, id, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*vfs.Entry, error) {
		f.Close()
		_ = root.RemovePart(dir, id)
		return nil, vfs.MapError(err)
	}
	if _, err := io.Copy(f, body); err != nil {
		return fail(err)
	}
	if s.cfg.Fsync {
		if err := f.Sync(); err != nil {
			return fail(err)
		}
	}
	if err := f.Close(); err != nil {
		_ = root.RemovePart(dir, id)
		return nil, vfs.MapError(err)
	}
	if mtime > 0 {
		_ = root.Chtimes(vfs.Join(dir, vfs.PartName(id)), uploads.ClampMtime(mtime))
	}
	if err := root.Finalize(dir, id, name, overwrite); err != nil {
		_ = root.RemovePart(dir, id)
		return nil, err
	}
	return root.Stat(vfs.Join(dir, name))
}

func (s *Server) acquireUpload(u *store.User) (func(), error) {
	key := strconv.FormatInt(u.ID, 10)
	if !s.uploadSem.TryAcquire(key) {
		return nil, errorf(http.StatusTooManyRequests, "busy", "too many concurrent uploads")
	}
	return func() { s.uploadSem.Release(key) }, nil
}

func (s *Server) handlePutContent(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := s.acquireUpload(u)
	if err != nil {
		return err
	}
	defer release()
	p, err := vfs.NormalizeWritable(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	if p == "" {
		return vfs.ErrRootOp
	}
	mtime, _ := strconv.ParseInt(r.URL.Query().Get("mtime"), 10, 64)
	if r.ContentLength > s.cfg.ChunkSize {
		return errorf(http.StatusRequestEntityTooLarge, "too_large", "use chunked upload for files larger than %d bytes", s.cfg.ChunkSize)
	}
	incoming := r.ContentLength
	if incoming < 0 {
		incoming = s.cfg.ChunkSize
	}
	if err := s.checkQuota(r.Context(), userFrom(r), incoming); err != nil {
		return err
	}
	e, err := s.storeSmallFile(root, vfs.Dir(p), vfs.Base(p), mtime, queryBool(r, "overwrite"), bodyReader(w, r, s.cfg.ChunkSize))
	if err != nil {
		return err
	}
	s.usageAdd(userFrom(r).ID, e.Size)
	s.indexTouch(indexAncestors(userFrom(r).Scope, vfs.Join(userFrom(r).Scope, p))...)
	writeJSON(w, r, 201, map[string]any{"entry": e, "path": p})
	return nil
}

type batchMeta struct {
	Files []struct {
		Path  string `json:"path"`
		Mtime int64  `json:"mtime"`
		Size  int64  `json:"size"`
	} `json:"files"`
}

type batchResult struct {
	Path  string     `json:"path"`
	OK    bool       `json:"ok"`
	Code  string     `json:"code,omitempty"`
	Error string     `json:"error,omitempty"`
	Entry *vfs.Entry `json:"entry,omitempty"`
}

// handleBatch accepts a multipart body: part "meta" (JSON manifest) followed by
// file parts named by their index in the manifest.
func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := s.acquireUpload(u)
	if err != nil {
		return err
	}
	defer release()
	if err := s.checkQuota(r.Context(), u, max64(r.ContentLength, 0)); err != nil {
		return err
	}
	dir, err := vfs.NormalizeWritable(r.URL.Query().Get("dir"))
	if err != nil {
		return err
	}
	overwrite := queryBool(r, "overwrite")
	r.Body = io.NopCloser(bodyReader(w, r, s.cfg.BatchMaxBytes+(1<<20)))
	mr, err := r.MultipartReader()
	if err != nil {
		return errorf(http.StatusBadRequest, "bad_multipart", "multipart body required")
	}
	var meta batchMeta
	results := []batchResult{}
	seen := map[int]bool{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return vfs.MapError(err)
		}
		name := part.FormName()
		if name == "meta" {
			if err := json.NewDecoder(io.LimitReader(part, maxJSONBody)).Decode(&meta); err != nil {
				return errorf(http.StatusBadRequest, "bad_meta", "invalid manifest")
			}
			if len(meta.Files) > s.cfg.BatchMaxFiles {
				return errorf(http.StatusBadRequest, "too_many_files", "at most %d files per batch", s.cfg.BatchMaxFiles)
			}
			results = make([]batchResult, len(meta.Files))
			for i, f := range meta.Files {
				results[i] = batchResult{Path: f.Path}
			}
			continue
		}
		idx, err := strconv.Atoi(name)
		if err != nil || idx < 0 || idx >= len(meta.Files) || seen[idx] {
			io.Copy(io.Discard, part)
			continue
		}
		seen[idx] = true
		spec := meta.Files[idx]
		rel, err := vfs.NormalizeWritable(spec.Path)
		if err != nil || rel == "" {
			results[idx].Code, results[idx].Error = "invalid_path", "invalid path"
			io.Copy(io.Discard, part)
			continue
		}
		full := vfs.Join(dir, rel)
		e, err := s.storeSmallFile(root, vfs.Dir(full), vfs.Base(full), spec.Mtime, overwrite, part)
		if err != nil {
			ae := toAPIError(err)
			results[idx].Code, results[idx].Error = ae.Code, ae.Message
			if ae.Status >= 500 || ae.Code == "no_space" {
				s.log.Warn("batch file failed", "path", full, "err", err)
			}
			io.Copy(io.Discard, part)
			continue
		}
		results[idx].OK, results[idx].Entry = true, e
	}
	for i := range results {
		if !seen[i] && results[i].Code == "" {
			results[i].Code, results[i].Error = "missing", "file part missing"
		}
	}
	var stored []string
	var added int64
	for _, res := range results {
		if res.OK {
			if res.Entry != nil {
				added += res.Entry.Size
			}
			stored = append(stored, indexAncestors(userFrom(r).Scope, vfs.Join(userFrom(r).Scope, dir, res.Path))...)
		}
	}
	s.usageAdd(u.ID, added)
	if len(stored) > 0 {
		s.indexTouch(stored...)
	}
	writeJSON(w, r, 200, map[string]any{"dir": dir, "results": results})
	return nil
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) error {
	root, _, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
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
	if err := root.MkdirAll(vfs.Dir(p)); err != nil {
		return err
	}
	if err := root.Mkdir(p); err != nil {
		return err
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	s.indexTouch(indexAncestors(userFrom(r).Scope, vfs.Join(userFrom(r).Scope, p))...)
	writeJSON(w, r, 201, map[string]any{"path": p, "entry": e})
	return nil
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) error {
	root, _, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	var in struct {
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := vfs.NormalizeWritable(in.Path)
	if err != nil {
		return err
	}
	dst, err := root.Rename(p, in.NewName)
	if err != nil {
		return err
	}
	_ = s.db.DeleteFavoriteByPath(r.Context(), userFrom(r).ID, vfs.Join(userFrom(r).Scope, p))
	e, err := root.Stat(dst)
	if err != nil {
		return err
	}
	s.indexRemove(vfs.Join(userFrom(r).Scope, p))
	s.indexTree(vfs.Join(userFrom(r).Scope, dst))
	writeJSON(w, r, 200, map[string]any{"path": dst, "entry": e})
	return nil
}

func normalizeWritableList(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errorf(http.StatusBadRequest, "no_paths", "no paths given")
	}
	if len(paths) > 10000 {
		return nil, errorf(http.StatusBadRequest, "too_many", "too many paths")
	}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		p, err := vfs.NormalizeWritable(raw)
		if err != nil {
			return nil, err
		}
		if p == "" {
			return nil, vfs.ErrRootOp
		}
		out = append(out, p)
	}
	return out, nil
}

func parseConflict(s string) (vfs.Conflict, error) {
	switch vfs.Conflict(s) {
	case "", vfs.ConflictRename:
		return vfs.ConflictRename, nil
	case vfs.ConflictOverwrite, vfs.ConflictSkip:
		return vfs.Conflict(s), nil
	}
	return "", errorf(http.StatusBadRequest, "bad_conflict", "onConflict must be rename, overwrite or skip")
}

// respondJob waits briefly for the job and returns its snapshot.
func respondJob(w http.ResponseWriter, r *http.Request, j *jobs.Job) error {
	j.Wait(300 * time.Millisecond)
	writeJSON(w, r, 200, map[string]any{"job": j.Snapshot()})
	return nil
}

func progressFor(j *jobs.Job) *vfs.Progress {
	return &vfs.Progress{
		Add:     j.Add,
		Current: j.SetCurrent,
		Warn:    func(p string, err error) { j.Warn(p + ": " + err.Error()) },
	}
}

func parentDirs(paths []string, extra ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, p := range paths {
		add(vfs.Dir(p))
	}
	for _, e := range extra {
		add(e)
	}
	return out
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		Paths     []string `json:"paths"`
		Permanent bool     `json:"permanent"` // ignora a lixeira
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	paths, err := normalizeWritableList(in.Paths)
	if err != nil {
		return err
	}
	// validate existence up-front so obvious errors are synchronous
	root, err := s.scopeRoot(u)
	if err != nil {
		return err
	}
	stats := make([]*vfs.Entry, len(paths))
	for i, p := range paths {
		if stats[i], err = root.Stat(p); err != nil {
			root.Close()
			return err
		}
	}
	root.Close()
	useTrash := s.cfg.TrashRetention > 0 && !in.Permanent
	j, err := s.startJob(u, "delete", jobLabel(paths, ""), parentDirs(paths), func(ctx context.Context, j *jobs.Job) error {
		root, err := s.scopeRoot(u)
		if err != nil {
			return err
		}
		defer root.Close()
		j.SetTotals(len(paths), 0)
		for i, p := range paths {
			j.SetCurrent(p)
			if useTrash {
				if err := s.trashOne(ctx, root, u, p, stats[i]); err != nil {
					return err
				}
			} else if err := root.RemoveTree(ctx, p, nil); err != nil {
				return err
			}
			_ = s.db.DeleteFavoriteByPath(context.Background(), u.ID, vfs.Join(u.Scope, p))
			s.indexRemove(vfs.Join(u.Scope, p))
			j.Add(1, 0)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return respondJob(w, r, j)
}

type transferReq struct {
	Sources    []string `json:"sources"`
	DestDir    string   `json:"destDir"`
	OnConflict string   `json:"onConflict"`
}

func (s *Server) parseTransfer(r *http.Request) ([]string, string, vfs.Conflict, error) {
	var in transferReq
	if err := readJSON(r, &in); err != nil {
		return nil, "", "", err
	}
	sources, err := normalizeWritableList(in.Sources)
	if err != nil {
		return nil, "", "", err
	}
	dest, err := vfs.NormalizeWritable(in.DestDir)
	if err != nil {
		return nil, "", "", err
	}
	policy, err := parseConflict(in.OnConflict)
	if err != nil {
		return nil, "", "", err
	}
	root, err := s.scopeRoot(userFrom(r))
	if err != nil {
		return nil, "", "", err
	}
	defer root.Close()
	if e, err := root.Stat(dest); err != nil {
		return nil, "", "", err
	} else if e.Type != "dir" {
		return nil, "", "", vfs.ErrNotDir
	}
	for _, src := range sources {
		if _, err := root.Stat(src); err != nil {
			return nil, "", "", err
		}
		if vfs.IsWithin(src, dest) {
			return nil, "", "", vfs.ErrNested
		}
	}
	return sources, dest, policy, nil
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	sources, dest, policy, err := s.parseTransfer(r)
	if err != nil {
		return err
	}
	j, err := s.startJob(u, "copy", jobLabel(sources, dest), parentDirs(nil, dest), func(ctx context.Context, j *jobs.Job) error {
		root, err := s.scopeRoot(u)
		if err != nil {
			return err
		}
		defer root.Close()
		var files int
		var bytes int64
		for _, src := range sources {
			t, err := root.Scan(ctx, src)
			if err != nil {
				return err
			}
			files += t.Files
			bytes += t.Bytes
		}
		if err := s.checkQuota(ctx, u, bytes); err != nil {
			return errors.New(toAPIError(err).Message) // vira o texto de erro do job
		}
		s.usageAdd(u.ID, bytes)
		j.SetTotals(files, bytes)
		prog := progressFor(j)
		for _, src := range sources {
			dst, ok, err := root.ResolveDest(src, dest, policy)
			if err != nil {
				return err
			}
			if !ok {
				j.Warn(src + ": skipped (exists)")
				continue
			}
			if err := root.CopyTree(ctx, src, dst, policy, prog); err != nil {
				return err
			}
			s.indexTree(vfs.Join(u.Scope, dst))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return respondJob(w, r, j)
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	sources, dest, policy, err := s.parseTransfer(r)
	if err != nil {
		return err
	}
	j, err := s.startJob(u, "move", jobLabel(sources, dest), parentDirs(sources, dest), func(ctx context.Context, j *jobs.Job) error {
		root, err := s.scopeRoot(u)
		if err != nil {
			return err
		}
		defer root.Close()
		j.SetTotals(len(sources), 0)
		prog := progressFor(j)
		for _, src := range sources {
			if err := ctx.Err(); err != nil {
				return err
			}
			j.SetCurrent(src)
			dst, ok, err := root.ResolveDest(src, dest, policy)
			if err != nil {
				return err
			}
			if !ok {
				j.Warn(src + ": skipped (exists)")
				continue
			}
			if err := s.moveOne(ctx, root, src, dst, policy, prog); err != nil {
				return err
			}
			_ = s.db.DeleteFavoriteByPath(context.Background(), u.ID, vfs.Join(u.Scope, src))
			s.indexRemove(vfs.Join(u.Scope, src))
			s.indexTree(vfs.Join(u.Scope, dst))
			j.Add(1, 0)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return respondJob(w, r, j)
}

// moveOne renames, merging directories on overwrite and falling back to copy+delete across devices.
func (s *Server) moveOne(ctx context.Context, root *vfs.Root, src, dst string, policy vfs.Conflict, prog *vfs.Progress) error {
	if policy == vfs.ConflictOverwrite {
		se, err1 := root.Stat(src)
		de, err2 := root.Stat(dst)
		if err1 == nil && err2 == nil && se.Type == "dir" && de.Type == "dir" {
			if err := root.CopyTree(ctx, src, dst, vfs.ConflictOverwrite, prog); err != nil {
				return err
			}
			return root.RemoveTree(ctx, src, nil)
		}
	}
	_, err := root.MoveTo(src, dst, policy == vfs.ConflictOverwrite)
	if err == nil {
		return nil
	}
	if !errors.Is(err, vfs.ErrCrossDevice) {
		return err
	}
	if err := root.CopyTree(ctx, src, dst, policy, prog); err != nil {
		return err
	}
	return root.RemoveTree(ctx, src, nil)
}

// Limites da pesquisa recursiva: entradas visitadas, resultados e tempo por requisição.
const (
	searchScanLimit  = 200_000
	searchMaxResults = 500
	searchTimeout    = 10 * time.Second
)

// handleSearch finds entries by name under ?path= within the user's scope.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 255 {
		return errorf(http.StatusBadRequest, "bad_query", "q is required (1-255 bytes)")
	}
	limit := searchMaxResults
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	key := strconv.FormatInt(u.ID, 10)
	if !s.searchSem.TryAcquire(key) {
		return errorf(http.StatusTooManyRequests, "busy", "too many concurrent searches")
	}
	defer s.searchSem.Release(key)
	if e, err := root.Stat(p); err != nil {
		return err
	} else if e.Type != "dir" {
		return vfs.ErrNotDir
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	if s.indexer != nil && s.indexer.Ready() {
		hits, partial, indexedAt, err := s.searchIndex(ctx, root, u.Scope, p, q, limit)
		if err != nil {
			return err
		}
		writeJSON(w, r, 200, map[string]any{"path": p, "q": q, "results": hits, "partial": partial, "source": "index", "indexedAt": indexedAt})
		return nil
	}
	hits, partial, err := root.Find(ctx, p, q, vfs.SearchLimits{MaxScan: searchScanLimit, MaxResults: limit})
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"path": p, "q": q, "results": hits, "partial": partial, "source": "walk", "indexedAt": nil})
	return nil
}

// searchIndex answers from the name index and drops ghosts (rows whose file vanished since
// the last scan) by re-checking each hit through the scope root.
func (s *Server) searchIndex(ctx context.Context, root *vfs.Root, scope, p, q string, limit int) ([]vfs.Found, bool, *int64, error) {
	rows, more, err := s.indexer.Search(ctx, vfs.Join(scope, p), q, limit)
	if err != nil {
		return nil, false, nil, err
	}
	hits := []vfs.Found{}
	for _, row := range rows {
		rel, ok := scopeRel(scope, row.Path)
		if !ok || rel == "" {
			continue
		}
		e, err := root.Stat(rel)
		if err != nil {
			continue // fantasma: sumiu desde a última varredura
		}
		hits = append(hits, vfs.Found{Dir: vfs.Dir(rel), Entry: *e})
	}
	var indexedAt *int64
	if st := s.indexer.Status(ctx); st.LastFullAt != nil {
		indexedAt = st.LastFullAt
	}
	return hits, more, indexedAt, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
