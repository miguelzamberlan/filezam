package server

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Padrões de fábrica. A tabela settings guarda só o que o administrador realmente mudou, então
// uma instalação nova sobe com estes valores e uma atualização não muda nada do que já existe.
const (
	defSlugsEnabled = true
	// O link de envio é escrita anônima no disco: inverte o modelo de ameaças do produto e
	// precisa ser uma escolha consciente de quem opera, nunca algo que aparece sozinho numa
	// atualização.
	defDropEnabled  = false
	defDropMaxQuota = int64(50) << 30
	defDropMaxTTL   = 30 * 24 * time.Hour
	defDropFileMax  = int64(2) << 30
	defDropMaxFiles = int64(500)
	defDropMaxLinks = int64(20)
	defDropStaleAge = 2 * time.Hour

	// dropTTLHardMax é requisito de produto, não configuração: um link de envio nunca pode
	// valer mais de 30 dias, e o administrador só consegue encurtar esse prazo.
	dropTTLHardMax = 30 * 24 * time.Hour
	// dropQuotaHardMax evita que um erro de digitação no painel transforme um link num ralo.
	dropQuotaHardMax = int64(1) << 40
	dropFilesHardMax = int64(100000)
	dropLinksHardMax = int64(1000)
)

// settings holds the admin-editable globals.
type settings struct {
	SlugsEnabled bool  `json:"slugsEnabled"`
	DropEnabled  bool  `json:"dropEnabled"`
	DropMaxQuota int64 `json:"dropMaxQuota"` // bytes
	DropMaxTTL   int64 `json:"dropMaxTtl"`   // segundos
	DropFileMax  int64 `json:"dropFileMax"`  // bytes
	DropMaxFiles int64 `json:"dropMaxFiles"`
	DropMaxLinks int64 `json:"dropMaxLinks"`
	DropStaleAge int64 `json:"dropStaleAge"` // segundos
}

func defaultSettings() settings {
	return settings{
		SlugsEnabled: defSlugsEnabled,
		DropEnabled:  defDropEnabled,
		DropMaxQuota: defDropMaxQuota,
		DropMaxTTL:   int64(defDropMaxTTL.Seconds()),
		DropFileMax:  defDropFileMax,
		DropMaxFiles: defDropMaxFiles,
		DropMaxLinks: defDropMaxLinks,
		DropStaleAge: int64(defDropStaleAge.Seconds()),
	}
}

// settingsCache keeps the values in memory: elas são lidas em toda criação de link e em toda
// requisição pública de envio, e não podem ir ao banco a cada vez.
type settingsCache struct {
	mu sync.RWMutex
	v  settings
}

func (s *Server) settings() settings {
	s.set.mu.RLock()
	defer s.set.mu.RUnlock()
	return s.set.v
}

// loadSettings fills the cache from the database, keeping the factory default for any key the
// administrator never touched (or that is no longer understood).
func (s *Server) loadSettings(ctx context.Context) error {
	rows, err := s.db.ListSettings(ctx)
	if err != nil {
		return err
	}
	v := defaultSettings()
	for _, row := range rows {
		switch row.Key {
		case "slugs_enabled":
			v.SlugsEnabled = row.Value == "1"
		case "drop_enabled":
			v.DropEnabled = row.Value == "1"
		case "drop_max_quota":
			v.DropMaxQuota = parseInt(row.Value, v.DropMaxQuota)
		case "drop_max_ttl":
			v.DropMaxTTL = parseInt(row.Value, v.DropMaxTTL)
		case "drop_file_max":
			v.DropFileMax = parseInt(row.Value, v.DropFileMax)
		case "drop_max_files":
			v.DropMaxFiles = parseInt(row.Value, v.DropMaxFiles)
		case "drop_max_links":
			v.DropMaxLinks = parseInt(row.Value, v.DropMaxLinks)
		case "drop_stale_age":
			v.DropStaleAge = parseInt(row.Value, v.DropStaleAge)
		}
	}
	s.set.mu.Lock()
	s.set.v = clampSettings(v)
	s.set.mu.Unlock()
	return nil
}

func parseInt(s string, def int64) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// clampSettings keeps stored values inside what the code can honour, so a hand-edited database
// never widens a limit beyond the hard ceiling.
func clampSettings(v settings) settings {
	v.DropMaxQuota = clamp64(v.DropMaxQuota, 1, dropQuotaHardMax)
	v.DropMaxTTL = clamp64(v.DropMaxTTL, 60, int64(dropTTLHardMax.Seconds()))
	v.DropFileMax = clamp64(v.DropFileMax, 1, v.DropMaxQuota)
	v.DropMaxFiles = clamp64(v.DropMaxFiles, 1, dropFilesHardMax)
	v.DropMaxLinks = clamp64(v.DropMaxLinks, 1, dropLinksHardMax)
	v.DropStaleAge = clamp64(v.DropStaleAge, 60, int64((24 * time.Hour).Seconds()))
	return v
}

func clamp64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// dropMaxTTL is the effective ceiling for a drop link: o menor entre a configuração, o teto
// rígido de 30 dias e a validade máxima de link público da instalação.
func (s *Server) dropMaxTTL() int64 {
	v := s.settings().DropMaxTTL
	return min(v, min(int64(dropTTLHardMax.Seconds()), int64(s.cfg.ShareMaxTTL.Seconds())))
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, 200, map[string]any{"settings": s.settings(), "dropTtlHardMax": int64(dropTTLHardMax.Seconds())})
	return nil
}

func (s *Server) handleAdminSettingsUpdate(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		SlugsEnabled *bool  `json:"slugsEnabled"`
		DropEnabled  *bool  `json:"dropEnabled"`
		DropMaxQuota *int64 `json:"dropMaxQuota"`
		DropMaxTTL   *int64 `json:"dropMaxTtl"`
		DropFileMax  *int64 `json:"dropFileMax"`
		DropMaxFiles *int64 `json:"dropMaxFiles"`
		DropMaxLinks *int64 `json:"dropMaxLinks"`
		DropStaleAge *int64 `json:"dropStaleAge"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	before := s.settings()
	vals := map[string]string{}
	putBool := func(key string, p *bool) {
		if p != nil {
			vals[key] = boolStr(*p)
		}
	}
	putInt := func(key string, p *int64, lo, hi int64) error {
		if p == nil {
			return nil
		}
		if *p < lo || *p > hi {
			return errorf(http.StatusBadRequest, "bad_quota", "%s must be between %d and %d", key, lo, hi)
		}
		vals[key] = strconv.FormatInt(*p, 10)
		return nil
	}
	putBool("slugs_enabled", in.SlugsEnabled)
	putBool("drop_enabled", in.DropEnabled)
	if err := putInt("drop_max_quota", in.DropMaxQuota, 1, dropQuotaHardMax); err != nil {
		return err
	}
	if err := putInt("drop_max_ttl", in.DropMaxTTL, 60, int64(dropTTLHardMax.Seconds())); err != nil {
		return err
	}
	if err := putInt("drop_file_max", in.DropFileMax, 1, dropQuotaHardMax); err != nil {
		return err
	}
	if err := putInt("drop_max_files", in.DropMaxFiles, 1, dropFilesHardMax); err != nil {
		return err
	}
	if err := putInt("drop_max_links", in.DropMaxLinks, 1, dropLinksHardMax); err != nil {
		return err
	}
	if err := putInt("drop_stale_age", in.DropStaleAge, 60, int64((24 * time.Hour).Seconds())); err != nil {
		return err
	}
	if len(vals) > 0 {
		if err := s.db.PutSettings(r.Context(), vals); err != nil {
			return err
		}
		if err := s.loadSettings(r.Context()); err != nil {
			return err
		}
	}
	after := s.settings()
	s.audit(r, u, "settings.update", map[string]any{"before": before, "after": after})
	writeJSON(w, r, 200, map[string]any{"settings": after, "dropTtlHardMax": int64(dropTTLHardMax.Seconds())})
	return nil
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// requireFeature refuses a disabled feature the same way for everyone, admin included: quem
// quiser usar liga primeiro nas configurações, e a mudança fica na auditoria.
func requireFeature(on bool) error {
	if on {
		return nil
	}
	return errorf(http.StatusForbidden, "feature_disabled", "this feature is disabled by the administrator")
}
