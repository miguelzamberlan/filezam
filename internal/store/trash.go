package store

import "context"

// TrashItem is a deleted entry kept in a trash folder until restored, purged or expired.
type TrashItem struct {
	ID        string
	UserID    int64
	UserName  string
	TrashDir  string // base-relative dir that holds <id>/<name>
	Name      string
	Path      string // original base-relative path
	Type      string
	Size      int64
	DeletedAt int64
}

const trashCols = `t.id, t.user_id, COALESCE(u.username,''), t.trash_dir, t.name, t.path, t.type, t.size, t.deleted_at`

func scanTrash(row interface{ Scan(...any) error }) (*TrashItem, error) {
	var t TrashItem
	if err := row.Scan(&t.ID, &t.UserID, &t.UserName, &t.TrashDir, &t.Name, &t.Path, &t.Type, &t.Size, &t.DeletedAt); err != nil {
		return nil, mapErr(err)
	}
	return &t, nil
}

func (db *DB) AddTrash(ctx context.Context, t *TrashItem) error {
	_, err := db.w.ExecContext(ctx, `INSERT INTO trash(id, user_id, trash_dir, name, path, type, size, deleted_at) VALUES(?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.TrashDir, t.Name, t.Path, t.Type, t.Size, t.DeletedAt)
	return mapErr(err)
}

func (db *DB) GetTrash(ctx context.Context, id string) (*TrashItem, error) {
	return scanTrash(db.r.QueryRowContext(ctx, `SELECT `+trashCols+` FROM trash t LEFT JOIN users u ON u.id=t.user_id WHERE t.id=?`, id))
}

// ListTrash lists items, newest first. userID<=0 lists every user's items.
func (db *DB) ListTrash(ctx context.Context, userID int64) ([]*TrashItem, error) {
	q := `SELECT ` + trashCols + ` FROM trash t LEFT JOIN users u ON u.id=t.user_id`
	var args []any
	if userID > 0 {
		q += ` WHERE t.user_id=?`
		args = append(args, userID)
	}
	return db.queryTrash(ctx, q+` ORDER BY t.deleted_at DESC, t.name`, args...)
}

// ListTrashBefore lists items deleted before the given unix time (for the retention sweep).
func (db *DB) ListTrashBefore(ctx context.Context, before int64) ([]*TrashItem, error) {
	return db.queryTrash(ctx, `SELECT `+trashCols+` FROM trash t LEFT JOIN users u ON u.id=t.user_id WHERE t.deleted_at<? ORDER BY t.deleted_at`, before)
}

func (db *DB) queryTrash(ctx context.Context, q string, args ...any) ([]*TrashItem, error) {
	rows, err := db.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TrashItem{}
	for rows.Next() {
		t, err := scanTrash(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) DeleteTrash(ctx context.Context, id string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM trash WHERE id=?`, id)
	return err
}
