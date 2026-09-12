package store

import (
	"context"
	"database/sql"

	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Share is a public link to a folder or file: read-only (mode "read") or an anonymous
// drop box (mode "drop", migração 011).
type Share struct {
	ID            int64
	TokenHash     string
	Token         string // token em claro; vazio nos links criados antes da migração 002
	Slug          string // apelido no lugar do token (migração 011); '' = só por token
	Mode          string // "read" | "drop" (migração 011)
	Kind          string // "dir" | "file" (migração 003)
	PasswordHash  string // Argon2id; vazio = sem senha
	Dev, Ino      uint64 // identidade do item no momento da criação (migração 006); 0 = desconhecida
	QuotaBytes    int64  // teto de bytes que o link de envio aceita; 0 fora do modo drop
	MaxFileBytes  int64  // teto por arquivo no modo drop
	MaxFiles      int64  // teto de arquivos no modo drop
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

// shareCols e scanShare mudam sempre juntos: um descompasso só aparece em runtime.
const shareCols = `s.id, s.token_hash, COALESCE(s.token,''), COALESCE(s.slug,''), COALESCE(s.mode,'read'), COALESCE(s.kind,'dir'), COALESCE(s.password_hash,''), COALESCE(s.dev,0), COALESCE(s.ino,0), COALESCE(s.quota_bytes,0), COALESCE(s.max_file_bytes,0), COALESCE(s.max_files,0), s.path, s.name, s.created_by, COALESCE(u.username,''), s.created_at, s.expires_at, s.revoked_at, s.access_count, s.last_access_at`

func scanShare(row interface{ Scan(...any) error }) (*Share, error) {
	var s Share
	var rev, last sql.NullInt64
	if err := row.Scan(&s.ID, &s.TokenHash, &s.Token, &s.Slug, &s.Mode, &s.Kind, &s.PasswordHash, &s.Dev, &s.Ino, &s.QuotaBytes, &s.MaxFileBytes, &s.MaxFiles, &s.Path, &s.Name, &s.CreatedBy, &s.CreatedByName, &s.CreatedAt, &s.ExpiresAt, &rev, &s.AccessCount, &last); err != nil {
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
	if s.Kind == "" {
		s.Kind = "dir"
	}
	if s.Mode == "" {
		s.Mode = "read"
	}
	res, err := db.w.ExecContext(ctx, `INSERT INTO shares(token_hash, token, slug, mode, kind, password_hash, dev, ino, quota_bytes, max_file_bytes, max_files, path, name, created_by, created_at, expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.TokenHash, s.Token, s.Slug, s.Mode, s.Kind, s.PasswordHash, int64(s.Dev), int64(s.Ino), s.QuotaBytes, s.MaxFileBytes, s.MaxFiles, s.Path, s.Name, s.CreatedBy, s.CreatedAt, s.ExpiresAt)
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
	// Um usuário desativado leva os links dele junto (voltam se ele for reativado).
	return scanShare(db.r.QueryRowContext(ctx, `SELECT `+shareCols+` FROM shares s LEFT JOIN users u ON u.id=s.created_by WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>? AND COALESCE(u.disabled,1)=0`, hash, db.now()))
}

// GetActiveShareBySlug fetches a live share by its slug (already canonical: lowercase).
func (db *DB) GetActiveShareBySlug(ctx context.Context, slug string) (*Share, error) {
	if slug == "" {
		return nil, ErrNotFound
	}
	return scanShare(db.r.QueryRowContext(ctx, `SELECT `+shareCols+` FROM shares s LEFT JOIN users u ON u.id=s.created_by WHERE s.slug=? AND s.revoked_at IS NULL AND s.expires_at>? AND COALESCE(u.disabled,1)=0`, slug, db.now()))
}

// SlugTaken reports whether a slug is claimed, revoked links included. Um apelido revogado
// continua preso ao dono original: liberá-lo deixaria outro usuário assumir um endereço já
// divulgado e passar a receber o que era destinado a quem o criou.
func (db *DB) SlugTaken(ctx context.Context, slug string) (bool, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares WHERE slug=?`, slug).Scan(&n)
	return n > 0, err
}

// DeleteSharesOutside removes the user's shares whose path is not within scope.
// scope "" keeps everything. Returns how many rows were deleted.
func (db *DB) DeleteSharesOutside(ctx context.Context, userID int64, scope string) (int, error) {
	shares, err := db.ListShares(ctx, userID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sh := range shares {
		if scope == "" || sh.Path == scope || vfs.IsWithin(scope, sh.Path) {
			continue
		}
		if err := db.DropShare(ctx, sh); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DeleteSharesUnder kills every share (any owner) of the base-relative path p or of anything
// below it. Comparação por bytes (substr), não LIKE: LIKE ignora maiúsculas. Links com apelido
// são revogados em vez de apagados, para que o endereço não volte ao pool (ver SlugTaken).
func (db *DB) DeleteSharesUnder(ctx context.Context, p string) (int, error) {
	if p == "" {
		return 0, nil
	}
	const where = ` WHERE (path=?1 OR substr(path, 1, length(?2)) = ?2)`
	res, err := db.w.ExecContext(ctx, `UPDATE shares SET revoked_at=?3`+where+` AND slug<>'' AND revoked_at IS NULL`, p, p+"/", db.now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	res, err = db.w.ExecContext(ctx, `DELETE FROM shares`+where+` AND slug=''`, p, p+"/")
	if err != nil {
		return int(n), err
	}
	d, _ := res.RowsAffected()
	return int(n + d), nil
}

// ListSharesUnder returns the shares of the base-relative path p or of anything below it.
func (db *DB) ListSharesUnder(ctx context.Context, p string) ([]*Share, error) {
	if p == "" {
		return nil, nil
	}
	rows, err := db.r.QueryContext(ctx, `SELECT `+shareCols+` FROM shares s LEFT JOIN users u ON u.id=s.created_by
		WHERE s.path=?1 OR substr(s.path, 1, length(?2)) = ?2`, p, p+"/")
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

// DeleteShare removes a share row, releasing its slug for anyone to claim.
func (db *DB) DeleteShare(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM shares WHERE id=?`, id)
	return err
}

// DropShare takes a link out of service: revoked when it holds a slug (the address stays
// reserved to its owner), deleted otherwise.
func (db *DB) DropShare(ctx context.Context, sh *Share) error {
	if sh.Slug == "" {
		return db.DeleteShare(ctx, sh.ID)
	}
	if err := db.RevokeShare(ctx, sh.ID); err != nil && err != ErrNotFound {
		return err
	}
	return nil
}

// TouchShare records an access. Best effort.
func (db *DB) TouchShare(ctx context.Context, id int64) {
	_, _ = db.w.ExecContext(ctx, `UPDATE shares SET access_count=access_count+1, last_access_at=? WHERE id=?`, db.now(), id)
}
