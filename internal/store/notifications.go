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
	"encoding/json"
	"errors"
	"strings"
)

// Notification is something that happened that a user should hear about.
type Notification struct {
	ID        int64          `json:"id"`
	UserID    int64          `json:"-"`
	Kind      string         `json:"kind"`
	GroupKey  string         `json:"-"`
	Data      map[string]any `json:"data"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
	ReadAt    *int64         `json:"readAt"`
}

// Notify records a notification. Com group != "", junta ao aviso ainda não lido do mesmo grupo:
// merge recebe os dados atuais (nil se não há) e devolve os novos. Tudo numa transação, então
// dois eventos simultâneos do mesmo grupo não viram duas notificações nem perdem contagem.
func (db *DB) Notify(ctx context.Context, userID int64, kind, group string, merge func(old map[string]any) map[string]any) error {
	tx, err := db.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := db.now()
	var id int64
	var raw string
	var old map[string]any
	if group != "" {
		err := tx.QueryRowContext(ctx, `SELECT id, data FROM notifications WHERE user_id=? AND group_key=? AND read_at IS NULL`, userID, group).Scan(&id, &raw)
		switch {
		case err == nil:
			_ = json.Unmarshal([]byte(raw), &old)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
	}
	b, err := json.Marshal(merge(old))
	if err != nil {
		return err
	}
	if id != 0 {
		_, err = tx.ExecContext(ctx, `UPDATE notifications SET data=?, updated_at=? WHERE id=?`, string(b), now, id)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO notifications(user_id, kind, group_key, data, created_at, updated_at) VALUES(?,?,?,?,?,?)`, userID, kind, group, string(b), now, now)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ListNotifications returns the user's latest notifications, newest first.
func (db *DB) ListNotifications(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT id, user_id, kind, group_key, data, created_at, updated_at, read_at FROM notifications WHERE user_id=? ORDER BY updated_at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var raw string
		var read sql.NullInt64
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.GroupKey, &raw, &n.CreatedAt, &n.UpdatedAt, &read); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &n.Data); err != nil || n.Data == nil {
			n.Data = map[string]any{}
		}
		if read.Valid {
			n.ReadAt = &read.Int64
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountUnreadNotifications counts the user's unread notifications.
func (db *DB) CountUnreadNotifications(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// MarkNotificationsRead marks the given notifications of the user as read; ids nil marks all.
func (db *DB) MarkNotificationsRead(ctx context.Context, userID int64, ids []int64) error {
	q := `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`
	args := []any{db.now(), userID}
	if ids != nil {
		if len(ids) == 0 {
			return nil
		}
		q += ` AND id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := db.w.ExecContext(ctx, q, args...)
	return err
}

// DeleteNotification removes one of the user's notifications.
func (db *DB) DeleteNotification(ctx context.Context, userID, id int64) error {
	res, err := db.w.ExecContext(ctx, `DELETE FROM notifications WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PruneNotifications deletes notifications last updated before the cutoff.
func (db *DB) PruneNotifications(ctx context.Context, before int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM notifications WHERE updated_at<?`, before)
	return err
}
