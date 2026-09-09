package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/store"
)

const (
	lockoutThreshold = 10
	lockoutBase      = 15 * time.Minute
)

// dummyHash equalizes timing for unknown usernames.
var dummyHash, _ = auth.HashPassword("filezam-dummy-password")

type userView struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	Restricted         bool   `json:"restricted"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

func viewUser(u *store.User) userView {
	return userView{ID: u.ID, Username: u.Username, Role: u.Role, Restricted: u.Scope != "", MustChangePassword: u.MustChangePassword}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || in.Password == "" || len(in.Username) > 64 || len(in.Password) > auth.MaxPasswordLen {
		return errorf(http.StatusBadRequest, "bad_credentials", "username and password required")
	}
	ip := ipFrom(r)
	if !s.loginIP.Allow("ip:"+ip) || !s.loginUser.Allow("user:"+strings.ToLower(in.Username)) {
		s.audit(r, nil, "login.ratelimited", map[string]any{"username": in.Username})
		return errorf(http.StatusTooManyRequests, "rate_limited", "too many login attempts; try again later")
	}
	if !s.loginSem.TryAcquire() {
		return errorf(http.StatusTooManyRequests, "busy", "server busy; try again")
	}
	defer s.loginSem.Release()

	ctx := r.Context()
	u, err := s.db.GetUserByName(ctx, in.Username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	invalid := errorf(http.StatusUnauthorized, "bad_credentials", "invalid username or password")
	if u == nil {
		auth.VerifyPassword(dummyHash, in.Password)
		s.audit(r, nil, "login.fail", map[string]any{"username": in.Username, "reason": "unknown"})
		return invalid
	}
	if u.LockedUntil != nil && time.Now().Unix() < *u.LockedUntil {
		auth.VerifyPassword(dummyHash, in.Password)
		s.audit(r, u, "login.locked", nil)
		return errorf(http.StatusLocked, "locked", "account temporarily locked; try again later")
	}
	if !auth.VerifyPassword(u.PasswordHash, in.Password) {
		until, _ := s.db.RecordLoginFailure(ctx, u.ID, lockoutThreshold, lockoutBase)
		detail := map[string]any{"reason": "password"}
		if until != nil {
			detail["lockedUntil"] = until.Unix()
		}
		s.audit(r, u, "login.fail", detail)
		return invalid
	}
	if u.Disabled {
		s.audit(r, u, "login.fail", map[string]any{"reason": "disabled"})
		return invalid
	}
	_ = s.db.RecordLoginSuccess(ctx, u.ID)
	tok, exp, err := s.createSession(ctx, r, u)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, r, tok, exp)
	s.audit(r, u, "login.ok", nil)
	writeJSON(w, r, 200, map[string]any{"user": viewUser(u)})
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if sess := sessionFrom(r); sess != nil {
		_ = s.db.DeleteSession(r.Context(), sess.ID)
	}
	s.clearSessionCookie(w, r)
	s.audit(r, userFrom(r), "logout", nil)
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, 200, map[string]any{"user": viewUser(userFrom(r))})
	return nil
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	u := userFrom(r)
	// Mesmas barreiras do login: quem roubou um cookie não pode testar senhas à vontade,
	// e cada verificação custa 64 MiB de Argon2.
	if !s.loginIP.Allow("ip:"+ipFrom(r)) || !s.loginUser.Allow("user:"+strings.ToLower(u.Username)) {
		s.audit(r, u, "password.ratelimited", nil)
		return errorf(http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
	}
	if !s.loginSem.TryAcquire() {
		return errorf(http.StatusTooManyRequests, "busy", "server busy; try again")
	}
	defer s.loginSem.Release()
	if !auth.VerifyPassword(u.PasswordHash, in.Current) {
		s.audit(r, u, "password.fail", nil)
		return errorf(http.StatusUnauthorized, "bad_credentials", "current password is incorrect")
	}
	if err := auth.CheckPolicy(in.New); err != nil {
		return errorf(http.StatusBadRequest, "weak_password", "%s", err.Error())
	}
	if in.New == in.Current {
		return errorf(http.StatusBadRequest, "weak_password", "new password must differ from the current one")
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	u.MustChangePassword = false
	if err := s.db.UpdateUser(r.Context(), u); err != nil {
		return err
	}
	if sess := sessionFrom(r); sess != nil {
		_ = s.db.DeleteUserSessions(r.Context(), u.ID, sess.ID)
	}
	s.audit(r, u, "password.change", nil)
	writeJSON(w, r, 200, map[string]any{"user": viewUser(u)})
	return nil
}
