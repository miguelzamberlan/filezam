package store

import (
	"context"
	"database/sql"
)

// Upload is a chunked upload session.
type Upload struct {
	ID        string
	UserID    int64
	Dir       string
	Name      string
	Size      int64
	Mtime     *int64
	ChunkSize int64
	Received  []byte
	Overwrite bool
	CreatedAt int64
	UpdatedAt int64
}

const uploadCols = `id, user_id, dir, name, size, mtime, chunk_size, received, overwrite, created_at, updated_at`

func scanUpload(row interface{ Scan(...any) error }) (*Upload, error) {
	var u Upload
	var mt sql.NullInt64
	if err := row.Scan(&u.ID, &u.UserID, &u.Dir, &u.Name, &u.Size, &mt, &u.ChunkSize, &u.Received, &u.Overwrite, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	if mt.Valid {
		u.Mtime = &mt.Int64
	}
	return &u, nil
}

// CreateUpload inserts a session.
func (db *DB) CreateUpload(ctx context.Context, u *Upload) error {
	now := db.now()
	u.CreatedAt, u.UpdatedAt = now, now
	_, err := db.w.ExecContext(ctx, `INSERT INTO uploads(`+uploadCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.UserID, u.Dir, u.Name, u.Size, nullInt(u.Mtime), u.ChunkSize, u.Received, u.Overwrite, u.CreatedAt, u.UpdatedAt)
	return mapErr(err)
}

// GetUpload fetches a session.
func (db *DB) GetUpload(ctx context.Context, id string) (*Upload, error) {
	return scanUpload(db.r.QueryRowContext(ctx, `SELECT `+uploadCols+` FROM uploads WHERE id=?`, id))
}

// ReservedBytes sums the declared sizes of the user's open sessions (pre-allocated on disk).
func (db *DB) ReservedBytes(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := db.r.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM uploads WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

// ListUploads lists sessions of a user (userID<=0: all).
func (db *DB) ListUploads(ctx context.Context, userID int64) ([]*Upload, error) {
	q := `SELECT ` + uploadCols + ` FROM uploads`
	var args []any
	if userID > 0 {
		q += ` WHERE user_id=?`
		args = append(args, userID)
	}
	q += ` ORDER BY created_at`
	rows, err := db.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Upload{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListStaleUploads returns sessions not updated since the given time.
func (db *DB) ListStaleUploads(ctx context.Context, before int64) ([]*Upload, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+uploadCols+` FROM uploads WHERE updated_at<?`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUploadReceived persists the bitset.
func (db *DB) UpdateUploadReceived(ctx context.Context, id string, received []byte) error {
	_, err := db.w.ExecContext(ctx, `UPDATE uploads SET received=?, updated_at=? WHERE id=?`, received, db.now(), id)
	return err
}

// DeleteUpload removes a session row.
func (db *DB) DeleteUpload(ctx context.Context, id string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, id)
	return err
}

// ActiveUploadIDs returns the set of session ids (for lazy part cleanup).
func (db *DB) ActiveUploadIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT id FROM uploads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
