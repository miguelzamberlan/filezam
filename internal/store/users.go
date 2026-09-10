package store

import (
	"context"
	"database/sql"
)

// User is a login account.
type User struct {
	ID                 int64
	Username           string
	PasswordHash       string
	Role               string
	Scope              string
	MustChangePassword bool
	Disabled           bool
	FailedLogins       int
	Lockouts           int
	LockedUntil        *int64
	Quota              int64  // bytes; 0 = sem limite (migração 007)
	TOTPSecret         string // cifrado (auth.Seal); vazio = 2FA desligado
	TOTPEnabledAt      *int64
	TOTPCounter        int64  // último intervalo TOTP aceito
	TOTPRecovery       string // JSON com hashes dos códigos de recuperação restantes
	CreatedAt          int64
	UpdatedAt          int64
}

// IsAdmin reports whether the user has the admin role.
func (u *User) IsAdmin() bool { return u.Role == "admin" }

const userCols = `id, username, password_hash, role, scope, must_change_password, disabled, failed_logins, lockouts, locked_until, created_at, updated_at, COALESCE(quota,0), COALESCE(totp_secret,''), totp_enabled_at, COALESCE(totp_counter,0), COALESCE(totp_recovery,'')`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var locked, totpAt sql.NullInt64
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Scope, &u.MustChangePassword, &u.Disabled, &u.FailedLogins, &u.Lockouts, &locked, &u.CreatedAt, &u.UpdatedAt, &u.Quota, &u.TOTPSecret, &totpAt, &u.TOTPCounter, &u.TOTPRecovery)
	if err != nil {
		return nil, mapErr(err)
	}
	if locked.Valid {
		u.LockedUntil = &locked.Int64
	}
	if totpAt.Valid {
		u.TOTPEnabledAt = &totpAt.Int64
	}
	return &u, nil
}

// CountUsers returns the number of users.
func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a user and returns it.
func (db *DB) CreateUser(ctx context.Context, u *User) (*User, error) {
	now := db.now()
	res, err := db.w.ExecContext(ctx, `INSERT INTO users(username, password_hash, role, scope, must_change_password, disabled, quota, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		u.Username, u.PasswordHash, u.Role, u.Scope, u.MustChangePassword, u.Disabled, u.Quota, now, now)
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return db.GetUser(ctx, id)
}

// GetUser fetches a user by id.
func (db *DB) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(db.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
}

// GetUserByName fetches a user by username (case-insensitive).
func (db *DB) GetUserByName(ctx context.Context, name string) (*User, error) {
	return scanUser(db.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username=?`, name))
}

// ListUsers returns all users ordered by username.
func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns the number of enabled admins.
func (db *DB) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0`).Scan(&n)
	return n, err
}

// UpdateUser persists mutable fields of a user.
func (db *DB) UpdateUser(ctx context.Context, u *User) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET password_hash=?, role=?, scope=?, must_change_password=?, disabled=?, quota=?, updated_at=? WHERE id=?`,
		u.PasswordHash, u.Role, u.Scope, u.MustChangePassword, u.Disabled, u.Quota, db.now(), u.ID)
	return mapErr(err)
}

// DeleteUser removes a user (cascades).
func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	res, err := db.w.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordLoginFailure counts a failed attempt (for the admin view). The lock itself is kept
// in memory per (user, IP) by auth.Lockout; locked_until is no longer written.
func (db *DB) RecordLoginFailure(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET failed_logins=failed_logins+1 WHERE id=?`, id)
	return err
}

// RecordLoginSuccess clears failure counters.
func (db *DB) RecordLoginSuccess(ctx context.Context, id int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET failed_logins=0, lockouts=0, locked_until=NULL WHERE id=?`, id)
	return err
}

// TOTPEnabled reports whether two-factor authentication is active.
func (u *User) TOTPEnabled() bool { return u.TOTPSecret != "" && u.TOTPEnabledAt != nil }

// SetTOTP stores (or clears, with secret "") the encrypted secret, enabling time and recovery hashes.
func (db *DB) SetTOTP(ctx context.Context, id int64, sealedSecret string, enabledAt *int64, recovery string) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET totp_secret=?, totp_enabled_at=?, totp_counter=0, totp_recovery=?, updated_at=? WHERE id=?`, sealedSecret, nullInt(enabledAt), recovery, db.now(), id)
	return err
}

// SetTOTPCounter records the last accepted interval (anti-replay).
func (db *DB) SetTOTPCounter(ctx context.Context, id, counter int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET totp_counter=? WHERE id=? AND totp_counter<?`, counter, id, counter)
	return err
}

// SetTOTPRecovery replaces the remaining recovery-code hashes.
func (db *DB) SetTOTPRecovery(ctx context.Context, id int64, recovery string) error {
	_, err := db.w.ExecContext(ctx, `UPDATE users SET totp_recovery=? WHERE id=?`, recovery, id)
	return err
}
