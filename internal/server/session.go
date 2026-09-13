// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/config"
	"github.com/miguelzamberlan/filezam/internal/store"
)

const sessionCookie = "filezam_session"

func (s *Server) secureCookie(r *http.Request) bool {
	switch s.cfg.SecureCookies {
	case config.SecureOn:
		return true
	case config.SecureOff:
		return false
	}
	return s.isHTTPS(r)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookie(r),
		SameSite: http.SameSiteStrictMode, Expires: expires,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// createSession issues a new session for the user.
func (s *Server) createSession(ctx context.Context, r *http.Request, u *store.User) (string, time.Time, error) {
	tok, err := auth.NewToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	exp := now.Add(s.cfg.SessionTTL)
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	sess := &store.Session{ID: auth.HashToken(tok), UserID: u.ID, CreatedAt: now.Unix(), ExpiresAt: exp.Unix(), LastSeenAt: now.Unix(), IP: ipFrom(r), UserAgent: ua}
	if err := s.db.CreateSession(ctx, sess); err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

// authenticate validates the session cookie and returns the user.
func (s *Server) authenticate(r *http.Request) (*store.User, *store.Session, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil, errUnauthorized
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, auth.HashToken(c.Value))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, errUnauthorized
		}
		return nil, nil, err
	}
	u, err := s.db.GetUser(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, errUnauthorized
		}
		return nil, nil, err
	}
	if u.Disabled {
		return nil, nil, errUnauthorized
	}
	now := time.Now()
	if now.Unix()-sess.LastSeenAt > 300 {
		exp := now.Add(s.cfg.SessionTTL)
		if absMax := time.Unix(sess.CreatedAt, 0).Add(s.cfg.SessionMaxTTL); exp.After(absMax) {
			exp = absMax
		}
		if exp.After(time.Unix(sess.ExpiresAt, 0)) || now.Unix()-sess.LastSeenAt > 300 {
			_ = s.db.TouchSession(ctx, sess.ID, now.Unix(), exp.Unix())
		}
	}
	return u, sess, nil
}
