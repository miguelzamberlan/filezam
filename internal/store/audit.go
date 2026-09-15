// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package store

import (
	"context"
	"database/sql"
	"strings"
)

// AuditEntry is one audit log row.
type AuditEntry struct {
	ID       int64  `json:"id"`
	TS       int64  `json:"ts"`
	UserID   *int64 `json:"userId"`
	Username string `json:"username"`
	IP       string `json:"ip"`
	Action   string `json:"action"`
	Detail   string `json:"detail"`
}

// AddAudit appends an audit entry.
func (db *DB) AddAudit(ctx context.Context, e *AuditEntry) error {
	_, err := db.w.ExecContext(ctx, `INSERT INTO audit_log(ts, user_id, username, ip, action, detail) VALUES(?,?,?,?,?,?)`,
		e.TS, nullInt(e.UserID), e.Username, e.IP, e.Action, e.Detail)
	return err
}

// ListAudit returns entries with id < before (0 = latest), newest first.
func (db *DB) ListAudit(ctx context.Context, before int64, limit int) ([]AuditEntry, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := db.r.QueryContext(ctx, `SELECT id, ts, user_id, COALESCE(username,''), COALESCE(ip,''), action, COALESCE(detail,'') FROM audit_log WHERE id<? ORDER BY id DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &uid, &e.Username, &e.IP, &e.Action, &e.Detail); err != nil {
			return nil, err
		}
		if uid.Valid {
			e.UserID = &uid.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAudit deletes entries older than ts.
func (db *DB) PruneAudit(ctx context.Context, olderThan int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM audit_log WHERE ts<?`, olderThan)
	return err
}

// CountAuditSince counts entries per action since ts, restricted to the given actions.
func (db *DB) CountAuditSince(ctx context.Context, since int64, actions ...string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(actions) == 0 {
		return out, nil
	}
	q := `SELECT action, COUNT(*) FROM audit_log WHERE ts>=? AND action IN (?` + strings.Repeat(",?", len(actions)-1) + `) GROUP BY action`
	args := []any{since}
	for _, a := range actions {
		args = append(args, a)
	}
	rows, err := db.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		var n int64
		if err := rows.Scan(&a, &n); err != nil {
			return nil, err
		}
		out[a] = n
	}
	return out, rows.Err()
}
