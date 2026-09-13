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

	"github.com/miguelzamberlan/filezam/internal/vfs"
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
	st, err := tx.PrepareContext(ctx, `INSERT INTO file_index(path, parent, name, name_fold, type, size, mtime, gen) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET parent=excluded.parent, name=excluded.name, name_fold=excluded.name_fold, type=excluded.type, size=excluded.size, mtime=excluded.mtime, gen=excluded.gen`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rows {
		if _, err := st.ExecContext(ctx, r.Path, parentOf(r.Path), r.Name, vfs.Fold(r.Name), r.Type, r.Size, r.Mtime, gen); err != nil {
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
// (substring compared through vfs.Fold: no case, no accents; % and _ in q are literal).
func (db *DB) SearchIndex(ctx context.Context, prefix, q string, limit int) ([]IndexRow, error) {
	args := []any{"%" + likeEscape(vfs.Fold(q)) + "%"}
	sqlq := `SELECT path, name, type, size, mtime FROM file_index WHERE name_fold LIKE ? ESCAPE '\'`
	if prefix != "" {
		sqlq += ` AND path LIKE ? ESCAPE '\'`
		args = append(args, likeEscape(prefix)+"/%")
	}
	sqlq += ` ORDER BY name_fold, path LIMIT ?`
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

const refoldBatch = 2000

// refoldIndex rewrites name_fold of every row when the stored fold version is behind
// vfs.FoldVersion, so an upgrade keeps answering from the index instead of waiting for the
// next full scan (which would leave accented searches missing until then). Runs once per
// version, in batches by rowid so a big index never sits in memory or in one transaction.
func (db *DB) refoldIndex(ctx context.Context) error {
	var fold int64
	if err := db.w.QueryRowContext(ctx, `SELECT fold FROM index_state WHERE id=1`).Scan(&fold); err != nil {
		return err
	}
	if fold >= vfs.FoldVersion {
		return nil
	}
	type row struct {
		id   int64
		name string
	}
	var last int64
	for {
		rs, err := db.w.QueryContext(ctx, `SELECT rowid, name FROM file_index WHERE rowid>? ORDER BY rowid LIMIT ?`, last, refoldBatch)
		if err != nil {
			return err
		}
		var batch []row
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.id, &r.name); err != nil {
				rs.Close()
				return err
			}
			batch = append(batch, r)
		}
		rs.Close()
		if err := rs.Err(); err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		tx, err := db.w.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, r := range batch {
			if _, err := tx.ExecContext(ctx, `UPDATE file_index SET name_fold=? WHERE rowid=?`, vfs.Fold(r.name), r.id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		last = batch[len(batch)-1].id
	}
	_, err := db.w.ExecContext(ctx, `UPDATE index_state SET fold=? WHERE id=1`, vfs.FoldVersion)
	return err
}

// IndexSumSize sums file sizes under prefix ("" = everything) from the index.
func (db *DB) IndexSumSize(ctx context.Context, prefix string) (int64, error) {
	var n sql.NullInt64
	q, args := `SELECT SUM(size) FROM file_index WHERE type='file'`, []any{}
	if prefix != "" {
		q += ` AND path LIKE ? ESCAPE '\'`
		args = append(args, likeEscape(prefix)+"/%")
	}
	if err := db.r.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n.Int64, nil
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
