package server

import (
	"context"
	"net/http"
	"time"

	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Ganchos do índice de nomes: cada mutação avisa o indexador em segundo plano (melhor
// esforço; a varredura periódica corrige o que escapar). Caminhos são base-relativos.

func (s *Server) indexAsync(fn func(ctx context.Context) error) {
	if s.indexer == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.bg, 10*time.Minute)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.log.Warn("index update", "err", err)
		}
	}()
}

func (s *Server) indexTouch(paths ...string) {
	s.indexAsync(func(ctx context.Context) error { return s.indexer.Touch(ctx, paths...) })
}

func (s *Server) indexTree(paths ...string) {
	s.indexAsync(func(ctx context.Context) error {
		for _, p := range paths {
			if err := s.indexer.ReindexTree(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) indexRemove(paths ...string) {
	s.indexAsync(func(ctx context.Context) error {
		for _, p := range paths {
			if err := s.indexer.RemoveTree(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
}

// indexAncestors lists p and its ancestors below stop (for files created inside new subfolders).
func indexAncestors(stop, p string) []string {
	var out []string
	for p != "" && p != stop {
		out = append(out, p)
		p = vfs.Dir(p)
	}
	return out
}

func (s *Server) handleAdminIndex(w http.ResponseWriter, r *http.Request) error {
	out := map[string]any{"enabled": s.indexer != nil}
	if s.indexer != nil {
		st := s.indexer.Status(r.Context())
		out["ready"], out["running"], out["entries"], out["lastFullAt"], out["interval"] = st.Ready, st.Running, st.Entries, st.LastFullAt, st.Interval
	}
	writeJSON(w, r, 200, out)
	return nil
}

func (s *Server) handleAdminReindex(w http.ResponseWriter, r *http.Request) error {
	if s.indexer == nil {
		return errorf(http.StatusConflict, "unsupported", "index disabled (FILEZAM_INDEX_INTERVAL=0)")
	}
	if s.indexer.Running() {
		writeJSON(w, r, 200, map[string]any{"started": false})
		return nil
	}
	s.audit(r, userFrom(r), "index.rebuild", nil)
	go func() {
		if _, err := s.indexer.FullScan(s.bg); err != nil {
			s.log.Warn("index rebuild", "err", err)
		}
	}()
	writeJSON(w, r, 200, map[string]any{"started": true})
	return nil
}
