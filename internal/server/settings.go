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

	// Extração e compactação. Os tetos são de dano, não de uso: o limite real do que cabe é a
	// cota do usuário. MaxArchive é o único que morde antes de o archive/zip carregar o
	// diretório central inteiro na memória.
	defExtractEnabled    = true
	defExtractMaxBytes   = int64(10) << 30
	defExtractMaxEntries = int64(50000)
	defExtractMaxArchive = int64(2) << 30

	// Miniaturas. O teto de pixels é lido do cabeçalho antes de decodificar, que é o que impede
	// uma imagem declarada como enorme de estourar a memória.
	defThumbsEnabled   = true
	defThumbsMaxPixels = int64(50_000_000)
	defThumbsMaxFile   = int64(64) << 20
	defThumbsCacheMax  = int64(2) << 30

	// Análise de mídia. Ler o cabeçalho é barato por arquivo e caro por pasta: o teto de
	// varredura é o que decide quando uma análise volta completa e quando volta parcial.
	defMediaEnabled = true
	defMediaMaxScan = int64(200_000)

	// dropTTLHardMax é requisito de produto, não configuração: um link de envio nunca pode
	// valer mais de 30 dias, e o administrador só consegue encurtar esse prazo.
	dropTTLHardMax = 30 * 24 * time.Hour
	// dropQuotaHardMax evita que um erro de digitação no painel transforme um link num ralo.
	dropQuotaHardMax = int64(1) << 40
	dropFilesHardMax = int64(100000)
	dropLinksHardMax = int64(1000)

	extractBytesHardMax   = int64(1) << 40
	extractEntriesHardMax = int64(500000)
	extractArchiveHardMax = int64(16) << 30

	// Retenção da lixeira e intervalo do índice. O padrão de fábrica é o das variáveis de ambiente
	// (FILEZAM_TRASH_RETENTION, FILEZAM_INDEX_INTERVAL), que continuam valendo até o administrador
	// escolher outro valor no painel: quem já configurava por ambiente não vê nada mudar.
	trashRetentionHardMax = int64(365 * 24 * 3600)
	indexIntervalMin      = int64(15 * 60)
	indexIntervalMax      = int64(7 * 24 * 3600)
	// settingUnset marca, na memória, uma chave que o administrador nunca gravou.
	settingUnset = int64(-1)

	mediaScanHardMax = int64(5_000_000)

	thumbsPixelsHardMax = int64(500_000_000)
	thumbsFileHardMax   = int64(1) << 30
	thumbsCacheHardMax  = int64(100) << 30
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

	ExtractEnabled    bool  `json:"extractEnabled"`
	ExtractMaxBytes   int64 `json:"extractMaxBytes"`   // total escrito por extração
	ExtractMaxEntries int64 `json:"extractMaxEntries"` // entradas por arquivo
	ExtractMaxArchive int64 `json:"extractMaxArchive"` // tamanho do arquivo de origem

	// MediaEnabled liga a análise técnica de foto e vídeo; MediaMaxScan é quantas entradas
	// uma análise de pasta visita antes de devolver um resultado parcial.
	MediaEnabled bool  `json:"mediaEnabled"`
	MediaMaxScan int64 `json:"mediaMaxScan"`

	ThumbsEnabled   bool  `json:"thumbsEnabled"`
	ThumbsMaxPixels int64 `json:"thumbsMaxPixels"`
	ThumbsMaxFile   int64 `json:"thumbsMaxFile"`
	ThumbsCacheMax  int64 `json:"thumbsCacheMax"`

	// Segundos. TrashRetention 0 = lixeira desligada (excluir apaga de vez). Os dois saem sempre
	// resolvidos em settings(): settingUnset só existe dentro do cache.
	TrashRetention int64 `json:"trashRetention"`
	IndexInterval  int64 `json:"indexInterval"`
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

		ExtractEnabled:    defExtractEnabled,
		ExtractMaxBytes:   defExtractMaxBytes,
		ExtractMaxEntries: defExtractMaxEntries,
		ExtractMaxArchive: defExtractMaxArchive,

		MediaEnabled: defMediaEnabled,
		MediaMaxScan: defMediaMaxScan,

		ThumbsEnabled:   defThumbsEnabled,
		ThumbsMaxPixels: defThumbsMaxPixels,
		ThumbsMaxFile:   defThumbsMaxFile,
		ThumbsCacheMax:  defThumbsCacheMax,

		TrashRetention: settingUnset,
		IndexInterval:  settingUnset,
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
	v := s.set.v
	s.set.mu.RUnlock()
	if v.TrashRetention == settingUnset {
		v.TrashRetention = int64(s.cfg.TrashRetention.Seconds())
	}
	if v.IndexInterval == settingUnset {
		v.IndexInterval = int64(s.cfg.IndexInterval.Seconds())
	}
	return v
}

// trashRetention is how long deleted items stay in the trash; 0 disables the trash.
func (s *Server) trashRetention() time.Duration {
	return time.Duration(s.settings().TrashRetention) * time.Second
}

// indexInterval is the time between full scans of the name index. Nunca menos que o mínimo:
// um valor de ambiente minúsculo não pode transformar a varredura num laço sem pausa.
func (s *Server) indexInterval() time.Duration {
	return time.Duration(max(s.settings().IndexInterval, indexIntervalMin)) * time.Second
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
		case "extract_enabled":
			v.ExtractEnabled = row.Value == "1"
		case "extract_max_bytes":
			v.ExtractMaxBytes = parseInt(row.Value, v.ExtractMaxBytes)
		case "extract_max_entries":
			v.ExtractMaxEntries = parseInt(row.Value, v.ExtractMaxEntries)
		case "extract_max_archive":
			v.ExtractMaxArchive = parseInt(row.Value, v.ExtractMaxArchive)
		case "media_enabled":
			v.MediaEnabled = row.Value == "1"
		case "media_max_scan":
			v.MediaMaxScan = parseInt(row.Value, v.MediaMaxScan)
		case "thumbs_enabled":
			v.ThumbsEnabled = row.Value == "1"
		case "thumbs_max_pixels":
			v.ThumbsMaxPixels = parseInt(row.Value, v.ThumbsMaxPixels)
		case "thumbs_max_file":
			v.ThumbsMaxFile = parseInt(row.Value, v.ThumbsMaxFile)
		case "thumbs_cache_max":
			v.ThumbsCacheMax = parseInt(row.Value, v.ThumbsCacheMax)
		case "trash_retention":
			v.TrashRetention = parseInt(row.Value, v.TrashRetention)
		case "index_interval":
			v.IndexInterval = parseInt(row.Value, v.IndexInterval)
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
	v.ExtractMaxBytes = clamp64(v.ExtractMaxBytes, 1<<20, extractBytesHardMax)
	v.ExtractMaxEntries = clamp64(v.ExtractMaxEntries, 1, extractEntriesHardMax)
	v.ExtractMaxArchive = clamp64(v.ExtractMaxArchive, 1<<20, extractArchiveHardMax)
	v.MediaMaxScan = clamp64(v.MediaMaxScan, 100, mediaScanHardMax)
	v.ThumbsMaxPixels = clamp64(v.ThumbsMaxPixels, 1<<16, thumbsPixelsHardMax)
	v.ThumbsMaxFile = clamp64(v.ThumbsMaxFile, 1<<16, thumbsFileHardMax)
	v.ThumbsCacheMax = clamp64(v.ThumbsCacheMax, 1<<20, thumbsCacheHardMax)
	if v.TrashRetention != settingUnset {
		v.TrashRetention = clamp64(v.TrashRetention, 0, trashRetentionHardMax)
	}
	if v.IndexInterval != settingUnset {
		v.IndexInterval = clamp64(v.IndexInterval, indexIntervalMin, indexIntervalMax)
	}
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

		ExtractEnabled    *bool  `json:"extractEnabled"`
		ExtractMaxBytes   *int64 `json:"extractMaxBytes"`
		ExtractMaxEntries *int64 `json:"extractMaxEntries"`
		ExtractMaxArchive *int64 `json:"extractMaxArchive"`

		MediaEnabled *bool  `json:"mediaEnabled"`
		MediaMaxScan *int64 `json:"mediaMaxScan"`

		ThumbsEnabled   *bool  `json:"thumbsEnabled"`
		ThumbsMaxPixels *int64 `json:"thumbsMaxPixels"`
		ThumbsMaxFile   *int64 `json:"thumbsMaxFile"`
		ThumbsCacheMax  *int64 `json:"thumbsCacheMax"`

		TrashRetention *int64 `json:"trashRetention"`
		IndexInterval  *int64 `json:"indexInterval"`
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
	putBool("extract_enabled", in.ExtractEnabled)
	if err := putInt("extract_max_bytes", in.ExtractMaxBytes, 1<<20, extractBytesHardMax); err != nil {
		return err
	}
	if err := putInt("extract_max_entries", in.ExtractMaxEntries, 1, extractEntriesHardMax); err != nil {
		return err
	}
	if err := putInt("extract_max_archive", in.ExtractMaxArchive, 1<<20, extractArchiveHardMax); err != nil {
		return err
	}
	putBool("media_enabled", in.MediaEnabled)
	if err := putInt("media_max_scan", in.MediaMaxScan, 100, mediaScanHardMax); err != nil {
		return err
	}
	putBool("thumbs_enabled", in.ThumbsEnabled)
	if err := putInt("thumbs_max_pixels", in.ThumbsMaxPixels, 1<<16, thumbsPixelsHardMax); err != nil {
		return err
	}
	if err := putInt("thumbs_max_file", in.ThumbsMaxFile, 1<<16, thumbsFileHardMax); err != nil {
		return err
	}
	if err := putInt("thumbs_cache_max", in.ThumbsCacheMax, 1<<20, thumbsCacheHardMax); err != nil {
		return err
	}
	if err := putInt("trash_retention", in.TrashRetention, 0, trashRetentionHardMax); err != nil {
		return err
	}
	if err := putInt("index_interval", in.IndexInterval, indexIntervalMin, indexIntervalMax); err != nil {
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
	if after.IndexInterval != before.IndexInterval {
		s.wakeIndex()
	}
	s.audit(r, u, "settings.update", map[string]any{"before": before, "after": after})
	writeJSON(w, r, 200, map[string]any{"settings": after, "dropTtlHardMax": int64(dropTTLHardMax.Seconds())})
	return nil
}

// wakeIndex asks the index loop to re-arm its timer (intervalo mudou, ou uma varredura manual terminou).
func (s *Server) wakeIndex() {
	select {
	case s.indexWake <- struct{}{}:
	default:
	}
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
