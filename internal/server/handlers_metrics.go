// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/miguelzamberlan/filezam/internal/metrics"
)

// GET /metrics: Prometheus text format. Enabled only with FILEZAM_METRICS_TOKEN; accepts
// `Authorization: Bearer <token>` or an admin session.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) error {
	if s.cfg.MetricsToken == "" {
		return errorf(http.StatusNotFound, "not_found", "metrics disabled")
	}
	ok := false
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		ok = subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(s.cfg.MetricsToken)) == 1
	}
	if !ok {
		if u, _, err := s.authenticate(r); err == nil && u.IsAdmin() {
			ok = true
		}
	}
	if !ok {
		return errUnauthorized
	}
	ctx := r.Context()
	var gauges []metrics.Gauge
	add := func(name, help, labels string, v float64) {
		gauges = append(gauges, metrics.Gauge{Name: name, Help: help, Labels: labels, Value: v})
	}
	add("filezam_build_info", "Build information", `version="`+s.version+`"`, 1)
	if n, err := s.db.CountUsers(ctx); err == nil {
		add("filezam_users_total", "Registered users", "", float64(n))
	}
	if n, err := s.db.CountSessions(ctx); err == nil {
		add("filezam_sessions_active", "Live sessions", "", float64(n))
	}
	if list, err := s.db.ListLiveSessions(ctx); err == nil {
		seen := map[int64]bool{}
		for _, se := range list {
			if time.Now().Unix()-se.LastSeenAt <= int64(sessionActiveWindow.Seconds()) {
				seen[se.UserID] = true
			}
		}
		add("filezam_users_active", "Users seen in the last 10 minutes", "", float64(len(seen)))
	}
	add("filezam_upload_senders_active", "Users and drop links uploading now (activity in the last 2 minutes)", "", float64(len(s.activity.snapshot())))
	if ups, err := s.db.ListOpenUploads(ctx); err == nil {
		add("filezam_upload_sessions_open", "Open chunked upload sessions", "", float64(len(ups)))
	}
	if shares, err := s.db.ListShares(ctx, 0); err == nil {
		active := 0
		for _, sh := range shares {
			if sh.RevokedAt == nil && sh.ExpiresAt > s.db.Now().Unix() {
				active++
			}
		}
		add("filezam_shares_active", "Public links not expired nor revoked", "", float64(active))
	}
	if items, err := s.db.ListTrash(ctx, 0); err == nil {
		var bytes int64
		for _, it := range items {
			bytes += it.Size
		}
		add("filezam_trash_items", "Items in the trash", "", float64(len(items)))
		add("filezam_trash_bytes", "Bytes in the trash (files only)", "", float64(bytes))
	}
	if s.indexer != nil {
		st := s.indexer.Status(ctx)
		add("filezam_index_entries", "Rows in the file name index", "", float64(st.Entries))
		if st.LastFullAt != nil {
			add("filezam_index_last_scan_timestamp_seconds", "Unix time of the last full index scan", "", float64(*st.LastFullAt))
		}
	}
	if counts, err := s.db.CountJobs(ctx); err == nil {
		for st, n := range counts {
			add("filezam_jobs", "Background jobs by state (history included)", `state="`+st+`"`, float64(n))
		}
	}
	d := s.base.Disk()
	add("filezam_disk_total_bytes", "Filesystem size of FILEZAM_ROOT", "", float64(d.Total))
	add("filezam_disk_free_bytes", "Free bytes on FILEZAM_ROOT", "", float64(d.Free))
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	s.metrics.Write(w, gauges)
	return nil
}
