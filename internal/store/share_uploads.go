// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package store

import "context"

// ShareUpload is one file received through a public drop link.
type ShareUpload struct {
	ID        int64
	ShareID   int64
	Sender    string
	Name      string // nome final no disco, já resolvido contra colisões: é o que o dono vê
	SentName  string // nome que o remetente pediu; é o único que volta para ele (ver migração 011)
	Size      int64
	CreatedAt int64
}

// DropUsage is what a drop link has received so far: it is the single source of truth for the
// quota. Semântica: total recebido, não ocupado agora — o dono apagar arquivos não devolve cota.
type DropUsage struct {
	Bytes int64
	Count int64
}

// AddShareUpload records a received file.
func (db *DB) AddShareUpload(ctx context.Context, s *ShareUpload) error {
	s.CreatedAt = db.now()
	if s.SentName == "" {
		s.SentName = s.Name
	}
	res, err := db.w.ExecContext(ctx, `INSERT INTO share_uploads(share_id, sender, name, sent_name, size, created_at) VALUES(?,?,?,?,?,?)`,
		s.ShareID, s.Sender, s.Name, s.SentName, s.Size, s.CreatedAt)
	if err != nil {
		return mapErr(err)
	}
	s.ID, _ = res.LastInsertId()
	return nil
}

// ShareUsage sums what the link already received plus what its open sessions reserve on disk.
// As sessões abertas entram na conta porque pré-alocam o tamanho declarado: sem isso um
// visitante encheria o disco com sessões que nunca conclui.
func (db *DB) ShareUsage(ctx context.Context, shareID int64) (DropUsage, error) {
	var u DropUsage
	err := db.r.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0), COUNT(*) FROM share_uploads WHERE share_id=?`, shareID).Scan(&u.Bytes, &u.Count)
	if err != nil {
		return u, err
	}
	var openBytes, openCount int64
	if err := db.r.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0), COUNT(*) FROM uploads WHERE share_id=?`, shareID).Scan(&openBytes, &openCount); err != nil {
		return u, err
	}
	u.Bytes += openBytes
	u.Count += openCount
	return u, nil
}

// ListShareUploads returns what one sender uploaded through the link, newest first.
func (db *DB) ListShareUploads(ctx context.Context, shareID int64, sender string) ([]*ShareUpload, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT id, share_id, sender, name, sent_name, size, created_at FROM share_uploads
		WHERE share_id=? AND sender=? ORDER BY created_at DESC, id DESC`, shareID, sender)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ShareUpload{}
	for rows.Next() {
		var s ShareUpload
		if err := rows.Scan(&s.ID, &s.ShareID, &s.Sender, &s.Name, &s.SentName, &s.Size, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// CountOpenShareUploads counts the sender's open sessions on the link. A cota limita bytes,
// não inodes: sem este teto, milhares de sessões de 1 byte passariam na cota.
func (db *DB) CountOpenShareUploads(ctx context.Context, shareID int64, sender string) (int, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM uploads WHERE share_id=? AND sender=?`, shareID, sender).Scan(&n)
	return n, err
}

// ShareOpenNames returns the final names reserved by the link's in-flight sessions. Elas não
// existem no disco ainda (os bytes vão para um .part), mas o nome já está comprometido: sem
// isto, dois remetentes escolheriam o mesmo nome final e o segundo bateria no índice único de
// uploads — um erro que também contaria a ele que alguém está enviando aquele nome.
func (db *DB) ShareOpenNames(ctx context.Context, shareID int64) (map[string]bool, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT name FROM uploads WHERE share_id=?`, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// CountActiveDropLinks counts the user's live drop links.
func (db *DB) CountActiveDropLinks(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares WHERE created_by=? AND mode='drop' AND revoked_at IS NULL AND expires_at>?`, userID, db.now()).Scan(&n)
	return n, err
}

// ListUploadsByShare returns the open sessions of a link (to clean them up with it).
func (db *DB) ListUploadsByShare(ctx context.Context, shareID int64) ([]*Upload, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+uploadCols+` FROM uploads WHERE share_id=?`, shareID)
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

// DropTotalsSince counts the files received by every drop link since ts.
func (db *DB) DropTotalsSince(ctx context.Context, since int64) (DropUsage, error) {
	var u DropUsage
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size),0) FROM share_uploads WHERE created_at>=?`, since).Scan(&u.Count, &u.Bytes)
	return u, err
}
