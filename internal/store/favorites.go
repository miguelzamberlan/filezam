package store

import "context"

// Favorite is a bookmarked path.
type Favorite struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

// ListFavorites returns a user's favorites.
func (db *DB) ListFavorites(ctx context.Context, userID int64) ([]Favorite, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT id, user_id, path, name, created_at FROM favorites WHERE user_id=? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.ID, &f.UserID, &f.Path, &f.Name, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddFavorite inserts a favorite.
func (db *DB) AddFavorite(ctx context.Context, userID int64, path, name string) (*Favorite, error) {
	now := db.now()
	res, err := db.w.ExecContext(ctx, `INSERT INTO favorites(user_id, path, name, created_at) VALUES(?,?,?,?)`, userID, path, name, now)
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return &Favorite{ID: id, UserID: userID, Path: path, Name: name, CreatedAt: now}, nil
}

// DeleteFavorite removes a favorite owned by the user.
func (db *DB) DeleteFavorite(ctx context.Context, userID, id int64) error {
	res, err := db.w.ExecContext(ctx, `DELETE FROM favorites WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFavoriteByPath removes a favorite by path.
func (db *DB) DeleteFavoriteByPath(ctx context.Context, userID int64, path string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM favorites WHERE user_id=? AND path=?`, userID, path)
	return err
}
