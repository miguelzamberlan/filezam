package store

import (
	"context"
	"database/sql"
	"strings"
)

// IndexRow is one entry of the file name index (path is base-relative).
type IndexRow struct {
	Path  string
	Name  string
	Type  string
	Size  int64
	Mtime int64
}

// IndexState describes the last full scan.
type IndexState struct {
	LastFullAt *int64
	Entries    int64
	Gen        int64
}

func parentOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// likeEscape makes s safe as a LIKE literal (ESCAPE '\').
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// UpsertIndexBatch inserts or replaces rows in one transaction, stamping gen.
func (db *DB) UpsertIndexBatch(ctx context.Context, rows []IndexRow, gen int64) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := db.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO file_index(path, parent, name, name_lc, type, size, mtime, gen) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET parent=excluded.parent, name=excluded.name, name_lc=excluded.name_lc, type=excluded.type, size=excluded.size, mtime=excluded.mtime, gen=excluded.gen`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rows {
		if _, err := st.ExecContext(ctx, r.Path, parentOf(r.Path), r.Name, strings.ToLower(r.Name), r.Type, r.Size, r.Mtime, gen); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteIndexNotGen removes rows a full scan did not touch.
func (db *DB) DeleteIndexNotGen(ctx context.Context, gen int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM file_index WHERE gen<>?`, gen)
	return err
}

// DeleteIndexTree removes a path and everything below it ("" clears the index).
func (db *DB) DeleteIndexTree(ctx context.Context, path string) error {
	if path == "" {
		_, err := db.w.ExecContext(ctx, `DELETE FROM file_index`)
		return err
	}
	_, err := db.w.ExecContext(ctx, `DELETE FROM file_index WHERE path=? OR path LIKE ? ESCAPE '\'`, path, likeEscape(path)+"/%")
	return err
}

// DeleteIndexChildren removes the direct children of dir.
func (db *DB) DeleteIndexChildren(ctx context.Context, dir string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM file_index WHERE parent=?`, dir)
	return err
}

// SearchIndex returns up to limit rows under prefix ("" = everything) whose name contains q
// (case-insensitive substring; % and _ in q are literal).
func (db *DB) SearchIndex(ctx context.Context, prefix, q string, limit int) ([]IndexRow, error) {
	args := []any{"%" + likeEscape(strings.ToLower(q)) + "%"}
	sqlq := `SELECT path, name, type, size, mtime FROM file_index WHERE name_lc LIKE ? ESCAPE '\'`
	if prefix != "" {
		sqlq += ` AND path LIKE ? ESCAPE '\'`
		args = append(args, likeEscape(prefix)+"/%")
	}
	sqlq += ` ORDER BY name_lc, path LIMIT ?`
	args = append(args, limit)
	rows, err := db.r.QueryContext(ctx, sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IndexRow{}
	for rows.Next() {
		var r IndexRow
		if err := rows.Scan(&r.Path, &r.Name, &r.Type, &r.Size, &r.Mtime); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) IndexCount(ctx context.Context) (int64, error) {
	var n int64
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM file_index`).Scan(&n)
	return n, err
}

func (db *DB) GetIndexState(ctx context.Context) (*IndexState, error) {
	var st IndexState
	var last sql.NullInt64
	if err := db.r.QueryRowContext(ctx, `SELECT last_full_at, entries, gen FROM index_state WHERE id=1`).Scan(&last, &st.Entries, &st.Gen); err != nil {
		return nil, err
	}
	if last.Valid {
		st.LastFullAt = &last.Int64
	}
	return &st, nil
}

func (db *DB) SetIndexState(ctx context.Context, lastFullAt, entries, gen int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE index_state SET last_full_at=?, entries=?, gen=? WHERE id=1`, lastFullAt, entries, gen)
	return err
}
