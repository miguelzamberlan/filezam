package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/config"
	"github.com/zamberlan/filezam/internal/store"
)

// Verificação em duas etapas (TOTP) com códigos de recuperação e "confiar neste dispositivo".
//
// - O segredo TOTP fica cifrado no banco com a chave do servidor (FILEZAM_SECRET_KEY ou
//   <DataDir>/secret.key gerado no primeiro uso): um dump do banco sozinho não basta.
// - Login: senha certa → token pendente (5 min, preso ao IP) → /api/auth/totp com o código
//   (ou um código de recuperação) cria a sessão. Erros passam pelo mesmo bloqueio por
//   (usuário, IP) do login.
// - Cookie fz_trust = uid.exp.HMAC(chave, uid|exp|totp_enabled_at): 30 dias, sem tabela;
//   desligar ou recadastrar o 2FA muda totp_enabled_at e invalida todos os dispositivos.

const (
	pendingLoginTTL = 5 * time.Minute
	pendingSetupTTL = 10 * time.Minute
	trustCookie     = "fz_trust"
	trustDays       = 30
)

type pendingLogin struct {
	userID int64
	ip     string
	exp    time.Time
}

type pendingSetup struct {
	secret string
	exp    time.Time
}

type pendingState struct {
	mu     sync.Mutex
	logins map[string]pendingLogin // token → login à espera do código
	setups map[int64]pendingSetup  // userID → segredo ainda não confirmado
}

func newPendingState() pendingState {
	return pendingState{logins: map[string]pendingLogin{}, setups: map[int64]pendingSetup{}}
}

func (p *pendingState) newLogin(userID int64, ip string) (string, error) {
	tok, err := auth.NewToken(24)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, v := range p.logins {
		if now.After(v.exp) {
			delete(p.logins, k)
		}
	}
	p.logins[tok] = pendingLogin{userID: userID, ip: ip, exp: now.Add(pendingLoginTTL)}
	return tok, nil
}

// takeLogin returns the pending login for tok (removing it when consume is true).
func (p *pendingState) takeLogin(tok, ip string, consume bool) (pendingLogin, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.logins[tok]
	if !ok || time.Now().After(v.exp) || v.ip != ip {
		return pendingLogin{}, false
	}
	if consume {
		delete(p.logins, tok)
	}
	return v, true
}

func (p *pendingState) putSetup(userID int64, secret string) {
	p.mu.Lock()
	p.setups[userID] = pendingSetup{secret: secret, exp: time.Now().Add(pendingSetupTTL)}
	p.mu.Unlock()
}

func (p *pendingState) getSetup(userID int64) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.setups[userID]
	if !ok || time.Now().After(v.exp) {
		return "", false
	}
	return v.secret, true
}

func (p *pendingState) dropSetup(userID int64) {
	p.mu.Lock()
	delete(p.setups, userID)
	p.mu.Unlock()
}

// loadSecretKey returns the configured key or creates/reads <DataDir>/secret.key (0600).
func loadSecretKey(cfg *config.Config) ([]byte, error) {
	if len(cfg.SecretKey) == 32 {
		return cfg.SecretKey, nil
	}
	path := filepath.Join(cfg.DataDir, "secret.key")
	if b, err := os.ReadFile(path); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err == nil && len(key) == 32 {
			return key, nil
		}
		return nil, fmt.Errorf("%s: invalid content (expected 64 hex characters)", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	raw, err := auth.NewToken(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(raw))
	if err := os.WriteFile(path, []byte(hex.EncodeToString(sum[:])+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return sum[:], nil
}

func (s *Server) totpSecret(u *store.User) (string, error) {
	return auth.Open(s.secretKey, u.TOTPSecret)
}

// --- dispositivo confiável ---

func (s *Server) trustValue(u *store.User, exp int64) string {
	at := int64(0)
	if u.TOTPEnabledAt != nil {
		at = *u.TOTPEnabledAt
	}
	m := hmac.New(sha256.New, s.secretKey)
	fmt.Fprintf(m, "trust|%d|%d|%d", u.ID, exp, at)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) setTrustCookie(w http.ResponseWriter, r *http.Request, u *store.User) {
	exp := time.Now().Add(trustDays * 24 * time.Hour).Unix()
	val := fmt.Sprintf("%d.%d.%s", u.ID, exp, s.trustValue(u, exp))
	http.SetCookie(w, &http.Cookie{Name: trustCookie, Value: val, Path: "/", HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: trustDays * 86400})
}

// trustedDevice reports whether the request carries a valid trust cookie for u.
func (s *Server) trustedDevice(r *http.Request, u *store.User) bool {
	c, err := r.Cookie(trustCookie)
	if err != nil {
		return false
	}
	parts := strings.SplitN(c.Value, ".", 3)
	if len(parts) != 3 {
		return false
	}
	uid, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || uid != u.ID || time.Now().Unix() > exp {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(parts[2]), []byte(s.trustValue(u, exp))) == 1
}

// --- segunda etapa do login ---

func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token string `json:"token"`
		Code  string `json:"code"`
		Trust bool   `json:"trust"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ip := ipFrom(r)
	pl, ok := s.pending.takeLogin(in.Token, ip, false)
	if !ok {
		return errorf(http.StatusUnauthorized, "totp_expired", "login step expired; sign in again")
	}
	u, err := s.db.GetUser(r.Context(), pl.userID)
	if err != nil || !u.TOTPEnabled() || u.Disabled {
		return errorf(http.StatusUnauthorized, "totp_expired", "login step expired; sign in again")
	}
	lockKey := "totp|" + strings.ToLower(u.Username) + "|" + ip
	if locked, _ := s.lockout.Locked(lockKey); locked || !s.loginUser.Allow("user:"+strings.ToLower(u.Username)) {
		return errorf(http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
	}
	if !s.verifyTOTPOrRecovery(r, u, in.Code) {
		s.lockout.Fail(lockKey)
		s.audit(r, u, "totp.fail", nil)
		return errorf(http.StatusUnauthorized, "bad_totp", "invalid code")
	}
	s.pending.takeLogin(in.Token, ip, true)
	s.lockout.Reset(lockKey)
	tok, exp, err := s.createSession(r.Context(), r, u)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, r, tok, exp)
	if in.Trust {
		s.setTrustCookie(w, r, u)
	}
	s.audit(r, u, "login.ok", map[string]any{"totp": true, "trusted": in.Trust})
	s.metrics.Inc("filezam_logins_total", `result="ok"`, 1)
	writeJSON(w, r, 200, map[string]any{"user": s.viewUser(u)})
	return nil
}

// verifyTOTPOrRecovery accepts a 6-digit code (anti-replay) or a recovery code (single use).
func (s *Server) verifyTOTPOrRecovery(r *http.Request, u *store.User, code string) bool {
	code = strings.TrimSpace(code)
	secret, err := s.totpSecret(u)
	if err != nil {
		return false
	}
	if ok, counter := auth.VerifyTOTP(secret, code, time.Now(), uint64(u.TOTPCounter)); ok {
		_ = s.db.SetTOTPCounter(r.Context(), u.ID, int64(counter))
		return true
	}
	var hashes []string
	_ = json.Unmarshal([]byte(u.TOTPRecovery), &hashes)
	if rest, ok := auth.UseRecoveryCode(hashes, code); ok {
		b, _ := json.Marshal(rest)
		_ = s.db.SetTOTPRecovery(r.Context(), u.ID, string(b))
		s.audit(r, u, "totp.recovery", map[string]any{"remaining": len(rest)})
		return true
	}
	return false
}

// --- cadastro, confirmação, desativação ---

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		return err
	}
	s.pending.putSetup(u.ID, secret)
	writeJSON(w, r, 200, map[string]any{"secret": secret, "uri": auth.TOTPURI("Filezam", u.Username, secret)})
	return nil
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	secret, ok := s.pending.getSetup(u.ID)
	if !ok {
		return errorf(http.StatusConflict, "totp_setup_expired", "start the setup again")
	}
	okCode, counter := auth.VerifyTOTP(secret, in.Code, time.Now(), 0)
	if !okCode {
		return errorf(http.StatusUnauthorized, "bad_totp", "invalid code")
	}
	sealed, err := auth.Seal(s.secretKey, secret)
	if err != nil {
		return err
	}
	codes, hashes, err := auth.NewRecoveryCodes()
	if err != nil {
		return err
	}
	rec, _ := json.Marshal(hashes)
	now := time.Now().Unix()
	if err := s.db.SetTOTP(r.Context(), u.ID, sealed, &now, string(rec)); err != nil {
		return err
	}
	_ = s.db.SetTOTPCounter(r.Context(), u.ID, int64(counter))
	s.pending.dropSetup(u.ID)
	// outras sessões caem: a partir de agora todas passam pela segunda etapa
	if sess := sessionFrom(r); sess != nil {
		_ = s.db.DeleteUserSessions(r.Context(), u.ID, sess.ID)
	}
	s.audit(r, u, "totp.enable", nil)
	u.TOTPSecret, u.TOTPEnabledAt = sealed, &now
	writeJSON(w, r, 200, map[string]any{"user": s.viewUser(u), "recoveryCodes": codes})
	return nil
}

// totpAuthorize re-checks password and current code for sensitive changes.
func (s *Server) totpAuthorize(r *http.Request, u *store.User, password, code string) error {
	if !s.loginSem.TryAcquire() {
		return errorf(http.StatusTooManyRequests, "busy", "server busy; try again")
	}
	defer s.loginSem.Release()
	if !auth.VerifyPassword(u.PasswordHash, password) {
		return errorf(http.StatusUnauthorized, "bad_credentials", "wrong password")
	}
	if !s.verifyTOTPOrRecovery(r, u, code) {
		return errorf(http.StatusUnauthorized, "bad_totp", "invalid code")
	}
	return nil
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !u.TOTPEnabled() {
		return errorf(http.StatusConflict, "totp_not_enabled", "two-factor authentication is not enabled")
	}
	if err := s.totpAuthorize(r, u, in.Password, in.Code); err != nil {
		return err
	}
	if err := s.db.SetTOTP(r.Context(), u.ID, "", nil, ""); err != nil {
		return err
	}
	s.audit(r, u, "totp.disable", nil)
	u.TOTPSecret, u.TOTPEnabledAt = "", nil
	writeJSON(w, r, 200, map[string]any{"user": s.viewUser(u)})
	return nil
}

func (s *Server) handleTOTPRecovery(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !u.TOTPEnabled() {
		return errorf(http.StatusConflict, "totp_not_enabled", "two-factor authentication is not enabled")
	}
	if err := s.totpAuthorize(r, u, in.Password, in.Code); err != nil {
		return err
	}
	codes, hashes, err := auth.NewRecoveryCodes()
	if err != nil {
		return err
	}
	rec, _ := json.Marshal(hashes)
	if err := s.db.SetTOTPRecovery(r.Context(), u.ID, string(rec)); err != nil {
		return err
	}
	s.audit(r, u, "totp.recovery_reset", nil)
	writeJSON(w, r, 200, map[string]any{"recoveryCodes": codes})
	return nil
}

// handleAdminTOTPReset clears a user's 2FA (lost phone and recovery codes).
func (s *Server) handleAdminTOTPReset(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	u, err := s.db.GetUser(r.Context(), id)
	if err != nil {
		return err
	}
	if err := s.db.SetTOTP(r.Context(), u.ID, "", nil, ""); err != nil {
		return err
	}
	_ = s.db.DeleteUserSessions(r.Context(), u.ID, "")
	s.audit(r, userFrom(r), "totp.reset", map[string]any{"id": u.ID, "username": u.Username})
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
