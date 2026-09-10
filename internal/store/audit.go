package store

import (
	"context"
	"database/sql"
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
