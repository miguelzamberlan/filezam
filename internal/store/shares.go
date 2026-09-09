package store

import (
	"context"
	"database/sql"
)

// Share is a public read-only link to a folder.
type Share struct {
	ID            int64
	TokenHash     string
	Token         string // token em claro; vazio nos links criados antes da migração 002
	Path          string
	Name          string
	CreatedBy     int64
	CreatedByName string
	CreatedAt     int64
	ExpiresAt     int64
	RevokedAt     *int64
	AccessCount   int64
	LastAccessAt  *int64
}

const shareCols = `s.id, s.token_hash, COALESCE(s.token,''), s.path, s.name, s.created_by, COALESCE(u.username,''), s.created_at, s.expires_at, s.revoked_at, s.access_count, s.last_access_at`

func scanShare(row interface{ Scan(...any) error }) (*Share, error) {
	var s Share
	var rev, last sql.NullInt64
	if err := row.Scan(&s.ID, &s.TokenHash, &s.Token, &s.Path, &s.Name, &s.CreatedBy, &s.CreatedByName, &s.CreatedAt, &s.ExpiresAt, &rev, &s.AccessCount, &last); err != nil {
		return nil, mapErr(err)
	}
	if rev.Valid {
		s.RevokedAt = &rev.Int64
	}
	if last.Valid {
		s.LastAccessAt = &last.Int64
	}
	return &s, nil
}

// CreateShare inserts a share.
func (db *DB) CreateShare(ctx context.Context, s *Share) (*Share, error) {
	res, err := db.w.ExecContext(ctx, `INSERT INTO shares(token_hash, token, path, name, created_by, created_at, expires_at) VALUES(?,?,?,?,?,?,?)`,
		s.TokenHash, s.Token, s.Path, s.Name, s.CreatedBy, s.CreatedAt, s.ExpiresAt)
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return db.GetShare(ctx, id)
}

// GetShare fetches by id.
func (db *DB) GetShare(ctx context.Context, id int64) (*Share, error) {
	return scanShare(db.r.QueryRowContext(ctx, `SELECT `+shareCols+` FROM shares s LEFT JOIN users u ON u.id=s.created_by WHERE s.id=?`, id))
}

// GetActiveShareByToken fetches a live (not expired, not revoked) share by token hash.
func (db *DB) GetActiveShareByToken(ctx context.Context, hash string) (*Share, error) {
	return scanShare(db.r.QueryRowContext(ctx, `SELECT `+shareCols+` FROM shares s LEFT JOIN users u ON u.id=s.created_by WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>?`, hash, db.now()))
}

// ListShares lists shares; userID<=0 lists all.
func (db *DB) ListShares(ctx context.Context, userID int64) ([]*Share, error) {
	q := `SELECT ` + shareCols + ` FROM shares s LEFT JOIN users u ON u.id=s.created_by`
	var args []any
	if userID > 0 {
		q += ` WHERE s.created_by=?`
		args = append(args, userID)
	}
	q += ` ORDER BY s.created_at DESC`
	rows, err := db.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Share{}
	for rows.Next() {
		s, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListSharesByPath returns live shares for an exact base-relative path (userID<=0: any owner).
func (db *DB) ListSharesByPath(ctx context.Context, path string, userID int64) ([]*Share, error) {
	q := `SELECT ` + shareCols + ` FROM shares s LEFT JOIN users u ON u.id=s.created_by WHERE s.path=? AND s.revoked_at IS NULL AND s.expires_at>?`
	args := []any{path, db.now()}
	if userID > 0 {
		q += ` AND s.created_by=?`
		args = append(args, userID)
	}
	rows, err := db.r.QueryContext(ctx, q+` ORDER BY s.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Share{}
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// RevokeShare marks a share revoked.
func (db *DB) RevokeShare(ctx context.Context, id int64) error {
	res, err := db.w.ExecContext(ctx, `UPDATE shares SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, db.now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteShare removes a share row.
func (db *DB) DeleteShare(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM shares WHERE id=?`, id)
	return err
}

// TouchShare records an access. Best effort.
func (db *DB) TouchShare(ctx context.Context, id int64) {
	_, _ = db.w.ExecContext(ctx, `UPDATE shares SET access_count=access_count+1, last_access_at=? WHERE id=?`, db.now(), id)
}
