package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/store"
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
	TOTPEnabled        bool   `json:"totpEnabled"`
	TOTPRequired       bool   `json:"totpRequired"` // admin obrigado a cadastrar antes de usar o app
}

func (s *Server) viewUser(u *store.User) userView {
	return userView{ID: u.ID, Username: u.Username, Role: u.Role, Restricted: u.Scope != "", MustChangePassword: u.MustChangePassword,
		TOTPEnabled: u.TOTPEnabled(), TOTPRequired: s.cfg.Require2FA && u.IsAdmin() && !u.TOTPEnabled()}
}

// userLimitKey keys the per-account limiter by (username, IP).
func userLimitKey(username, ip string) string {
	return "user:" + strings.ToLower(username) + "|" + ip
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
	// O limitador por usuário é por (usuário, IP), como o bloqueio: um limitador só por nome
	// deixaria qualquer um trancar o admin de fora com 5 tentativas baratas por minuto.
	if !s.loginIP.Allow("ip:"+ip) || !s.loginUser.Allow(userLimitKey(in.Username, ip)) {
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
	fail := func() error {
		s.metrics.Inc("filezam_logins_total", `result="fail"`, 1)
		return invalid
	}
	if u == nil {
		auth.VerifyPassword(dummyHash, in.Password)
		s.audit(r, nil, "login.fail", map[string]any{"username": in.Username, "reason": "unknown"})
		return fail()
	}
	// Bloqueio por (usuário, IP): mesma resposta 401 de uma senha errada, para não revelar
	// que a conta existe, e sem afetar o mesmo usuário vindo de outro endereço.
	lockKey := strings.ToLower(u.Username) + "|" + ip
	if locked, until := s.lockout.Locked(lockKey); locked {
		auth.VerifyPassword(dummyHash, in.Password)
		s.audit(r, u, "login.locked", map[string]any{"until": until.Unix()})
		return fail()
	}
	if !auth.VerifyPassword(u.PasswordHash, in.Password) {
		_ = s.db.RecordLoginFailure(ctx, u.ID)
		detail := map[string]any{"reason": "password"}
		if locked, until := s.lockout.Fail(lockKey); locked {
			detail["lockedUntil"] = until.Unix()
		}
		s.audit(r, u, "login.fail", detail)
		return fail()
	}
	if u.Disabled {
		s.audit(r, u, "login.fail", map[string]any{"reason": "disabled"})
		return fail()
	}
	s.lockout.Reset(lockKey)
	_ = s.db.RecordLoginSuccess(ctx, u.ID)
	// Segunda etapa: com 2FA ativo e sem cookie de dispositivo confiável, devolve um token
	// temporário em vez da sessão; /api/auth/totp troca token + código pela sessão.
	if u.TOTPEnabled() && !s.trustedDevice(r, u) {
		ptok, err := s.pending.newLogin(u.ID, ip)
		if err != nil {
			return err
		}
		s.audit(r, u, "login.totp_pending", nil)
		writeJSON(w, r, 200, map[string]any{"totpRequired": true, "token": ptok})
		return nil
	}
	tok, exp, err := s.createSession(ctx, r, u)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, r, tok, exp)
	s.audit(r, u, "login.ok", nil)
	s.metrics.Inc("filezam_logins_total", `result="ok"`, 1)
	writeJSON(w, r, 200, map[string]any{"user": s.viewUser(u)})
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
	writeJSON(w, r, 200, map[string]any{"user": s.viewUser(userFrom(r))})
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
	if !s.loginIP.Allow("ip:"+ipFrom(r)) || !s.loginUser.Allow(userLimitKey(u.Username, ipFrom(r))) {
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
	writeJSON(w, r, 200, map[string]any{"user": s.viewUser(u)})
	return nil
}
