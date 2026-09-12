package store

import "context"

// Setting is one global configuration entry edited by an administrator.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt int64
}

// ListSettings returns every stored setting. Chaves ausentes ficam com o padrão de fábrica
// do servidor: a tabela guarda só o que o administrador realmente mudou.
func (db *DB) ListSettings(ctx context.Context) ([]Setting, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT key, value, updated_at FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Setting{}
	for rows.Next() {
		var s Setting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PutSettings writes the given keys in one transaction.
func (db *DB) PutSettings(ctx context.Context, vals map[string]string) error {
	tx, err := db.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := db.now()
	for k, v := range vals {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at) VALUES(?,?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, k, v, now); err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}
