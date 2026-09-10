package store

import (
	"context"
)

// Session is a server-side login session. ID is the hash of the client token.
type Session struct {
	ID         string
	UserID     int64
	CreatedAt  int64
	ExpiresAt  int64
	LastSeenAt int64
	IP         string
	UserAgent  string
}

// CreateSession inserts a session.
func (db *DB) CreateSession(ctx context.Context, s *Session) error {
	_, err := db.w.ExecContext(ctx, `INSERT INTO sessions(id, user_id, created_at, expires_at, last_seen_at, ip, user_agent) VALUES(?,?,?,?,?,?,?)`,
		s.ID, s.UserID, s.CreatedAt, s.ExpiresAt, s.LastSeenAt, s.IP, s.UserAgent)
	return mapErr(err)
}

// GetSession fetches a session by hashed id, if not expired.
func (db *DB) GetSession(ctx context.Context, id string) (*Session, error) {
	var s Session
	err := db.r.QueryRowContext(ctx, `SELECT id, user_id, created_at, expires_at, last_seen_at, COALESCE(ip,''), COALESCE(user_agent,'') FROM sessions WHERE id=? AND expires_at>?`, id, db.now()).
		Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt, &s.IP, &s.UserAgent)
	if err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

// TouchSession extends a session.
func (db *DB) TouchSession(ctx context.Context, id string, lastSeen, expires int64) error {
	_, err := db.w.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id=?`, lastSeen, expires, id)
	return err
}

// DeleteSession removes one session.
func (db *DB) DeleteSession(ctx context.Context, id string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	return err
}

// DeleteUserSessions removes all sessions of a user, optionally keeping one.
func (db *DB) DeleteUserSessions(ctx context.Context, userID int64, keep string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND id<>?`, userID, keep)
	return err
}

// PurgeExpiredSessions deletes expired sessions.
func (db *DB) PurgeExpiredSessions(ctx context.Context) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=?`, db.now())
	return err
}

// CountSessions counts sessions not yet expired.
func (db *DB) CountSessions(ctx context.Context) (int64, error) {
	var n int64
	err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE expires_at>?`, db.now()).Scan(&n)
	return n, err
}
