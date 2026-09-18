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
	"strings"
)

// MediaRow is one cached header reading (path is base-relative, as everywhere in the DB).
type MediaRow struct {
	Path     string
	Size     int64
	Mtime    int64 // unix ms
	Readable bool
	Meta     string // JSON de media.Meta; vazio quando Readable é falso
}

// mediaChunk é quantos caminhos entram numa cláusula IN. SQLite aceita bem mais, mas uma
// consulta gigante não ganha nada e prende o leitor por mais tempo.
const mediaChunk = 400

// GetMediaMeta reads the cached rows for the given paths, skipping the ones that no longer
// match the file on disk: o par tamanho + mtime é a validade da linha.
func (db *DB) GetMediaMeta(ctx context.Context, want map[string][2]int64) (map[string]MediaRow, error) {
	out := make(map[string]MediaRow, len(want))
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	for start := 0; start < len(paths); start += mediaChunk {
		end := min(start+mediaChunk, len(paths))
		batch := paths[start:end]
		args := make([]any, len(batch))
		for i, p := range batch {
			args[i] = p
		}
		q := `SELECT path, size, mtime, readable, meta FROM media_meta WHERE path IN (?` +
			strings.Repeat(",?", len(batch)-1) + `)`
		rows, err := db.r.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m MediaRow
			var readable int
			if err := rows.Scan(&m.Path, &m.Size, &m.Mtime, &readable, &m.Meta); err != nil {
				rows.Close()
				return nil, err
			}
			m.Readable = readable != 0
			if cur, ok := want[m.Path]; ok && cur[0] == m.Size && cur[1] == m.Mtime {
				out[m.Path] = m
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PutMediaMeta stores readings in one transaction.
func (db *DB) PutMediaMeta(ctx context.Context, rows []MediaRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := db.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO media_meta(path, size, mtime, readable, meta, read_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET size=excluded.size, mtime=excluded.mtime, readable=excluded.readable, meta=excluded.meta, read_at=excluded.read_at`)
	if err != nil {
		return err
	}
	defer st.Close()
	now := db.now()
	for _, r := range rows {
		readable := 0
		if r.Readable {
			readable = 1
		}
		if _, err := st.ExecContext(ctx, r.Path, r.Size, r.Mtime, readable, r.Meta, now); err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}

// DeleteMediaTree forgets a path and everything below it (o item saiu ou mudou de lugar).
func (db *DB) DeleteMediaTree(ctx context.Context, path string) error {
	if path == "" {
		_, err := db.w.ExecContext(ctx, `DELETE FROM media_meta`)
		return err
	}
	_, err := db.w.ExecContext(ctx, `DELETE FROM media_meta WHERE path = ? OR path LIKE ? ESCAPE '\'`,
		path, likeEscape(path)+`/%`)
	return err
}

// PruneMediaMeta drops readings whose file is no longer in the name index. Roda depois da
// varredura completa, que é quando o índice acabou de virar o retrato do disco.
func (db *DB) PruneMediaMeta(ctx context.Context) (int64, error) {
	res, err := db.w.ExecContext(ctx, `DELETE FROM media_meta WHERE path NOT IN (SELECT path FROM file_index)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountMediaMeta is how many readings are cached (painel do administrador).
func (db *DB) CountMediaMeta(ctx context.Context) (int64, error) {
	var n int64
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_meta`).Scan(&n)
	return n, err
}
