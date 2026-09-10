package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
)

type adminUserView struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	Scope              string `json:"scope"`
	MustChangePassword bool   `json:"mustChangePassword"`
	Disabled           bool   `json:"disabled"`
	LockedUntil        *int64 `json:"lockedUntil"`
	CreatedAt          int64  `json:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt"`
	Quota              int64  `json:"quota"` // bytes; 0 = sem limite
	TOTPEnabled        bool   `json:"totpEnabled"`
}

func viewAdminUser(u *store.User) adminUserView {
	return adminUserView{ID: u.ID, Username: u.Username, Role: u.Role, Scope: u.Scope, MustChangePassword: u.MustChangePassword,
		Disabled: u.Disabled, LockedUntil: u.LockedUntil, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, Quota: u.Quota, TOTPEnabled: u.TOTPEnabled()}
}

func validUsername(n string) bool {
	if len(n) < 2 || len(n) > 64 {
		return false
	}
	for _, c := range n {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '@') {
			return false
		}
	}
	return true
}

func (s *Server) validateScope(scope string) (string, error) {
	sc, err := vfs.NormalizeWritable(scope)
	if err != nil {
		return "", errorf(http.StatusBadRequest, "invalid_scope", "invalid scope path")
	}
	if sc == "" {
		return "", nil
	}
	e, err := s.base.Stat(sc)
	if err != nil {
		if errors.Is(err, vfs.ErrNotFound) {
			return "", errorf(http.StatusBadRequest, "invalid_scope", "scope folder does not exist")
		}
		return "", err
	}
	if e.Type != "dir" || e.Link {
		return "", errorf(http.StatusBadRequest, "invalid_scope", "scope must be a real directory")
	}
	return sc, nil
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) error {
	us, err := s.db.ListUsers(r.Context())
	if err != nil {
		return err
	}
	out := make([]adminUserView, 0, len(us))
	for _, u := range us {
		out = append(out, viewAdminUser(u))
	}
	writeJSON(w, r, 200, map[string]any{"users": out})
	return nil
}

func (s *Server) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Username           string `json:"username"`
		Password           string `json:"password"`
		Role               string `json:"role"`
		Scope              string `json:"scope"`
		MustChangePassword bool   `json:"mustChangePassword"`
		Quota              int64  `json:"quota"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Username = strings.TrimSpace(in.Username)
	if !validUsername(in.Username) {
		return errorf(http.StatusBadRequest, "invalid_username", "username must be 2-64 chars of letters, digits, . _ - @")
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.Role != "admin" && in.Role != "user" {
		return errorf(http.StatusBadRequest, "invalid_role", "role must be admin or user")
	}
	if err := auth.CheckPolicy(in.Password); err != nil {
		return errorf(http.StatusBadRequest, "weak_password", "%s", err.Error())
	}
	scope, err := s.validateScope(in.Scope)
	if err != nil {
		return err
	}
	if in.Quota < 0 {
		return errorf(http.StatusBadRequest, "bad_quota", "quota must be >= 0")
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return err
	}
	u, err := s.db.CreateUser(r.Context(), &store.User{Username: in.Username, PasswordHash: hash, Role: in.Role, Scope: scope, MustChangePassword: in.MustChangePassword, Quota: in.Quota})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return errorf(http.StatusConflict, "exists", "username already exists")
		}
		return err
	}
	s.audit(r, userFrom(r), "user.create", map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "scope": u.Scope})
	writeJSON(w, r, 201, map[string]any{"user": viewAdminUser(u)})
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errorf(http.StatusBadRequest, "bad_id", "invalid id")
	}
	return id, nil
}

func (s *Server) handleAdminUserUpdate(w http.ResponseWriter, r *http.Request) error {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Role               *string `json:"role"`
		Scope              *string `json:"scope"`
		Disabled           *bool   `json:"disabled"`
		Password           *string `json:"password"`
		MustChangePassword *bool   `json:"mustChangePassword"`
		Quota              *int64  `json:"quota"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	u, err := s.db.GetUser(ctx, id)
	if err != nil {
		return err
	}
	revoke := false
	changes := map[string]any{"id": id, "username": u.Username}
	if in.Role != nil && *in.Role != u.Role {
		if *in.Role != "admin" && *in.Role != "user" {
			return errorf(http.StatusBadRequest, "invalid_role", "role must be admin or user")
		}
		if u.IsAdmin() && !u.Disabled {
			if n, _ := s.db.CountAdmins(ctx); n <= 1 {
				return errorf(http.StatusConflict, "last_admin", "cannot demote the last admin")
			}
		}
		u.Role = *in.Role
		changes["role"] = u.Role
		revoke = true
	}
	if in.Scope != nil {
		sc, err := s.validateScope(*in.Scope)
		if err != nil {
			return err
		}
		if sc != u.Scope {
			u.Scope = sc
			changes["scope"] = sc
			revoke = true
		}
	}
	if in.Disabled != nil && *in.Disabled != u.Disabled {
		if *in.Disabled && u.IsAdmin() {
			if n, _ := s.db.CountAdmins(ctx); n <= 1 {
				return errorf(http.StatusConflict, "last_admin", "cannot disable the last admin")
			}
		}
		u.Disabled = *in.Disabled
		changes["disabled"] = u.Disabled
		revoke = true
	}
	if in.Password != nil {
		if err := auth.CheckPolicy(*in.Password); err != nil {
			return errorf(http.StatusBadRequest, "weak_password", "%s", err.Error())
		}
		hash, err := auth.HashPassword(*in.Password)
		if err != nil {
			return err
		}
		u.PasswordHash = hash
		changes["password"] = true
		revoke = true
	}
	if in.MustChangePassword != nil {
		u.MustChangePassword = *in.MustChangePassword
		changes["mustChangePassword"] = u.MustChangePassword
	}
	if in.Quota != nil && *in.Quota != u.Quota {
		if *in.Quota < 0 {
			return errorf(http.StatusBadRequest, "bad_quota", "quota must be >= 0")
		}
		u.Quota = *in.Quota
		changes["quota"] = u.Quota
	}
	if err := s.db.UpdateUser(ctx, u); err != nil {
		return err
	}
	if _, ok := changes["scope"]; ok {
		// links públicos de pastas que saíram do escopo morrem junto com o acesso
		if n, err := s.db.DeleteSharesOutside(ctx, u.ID, u.Scope); err == nil && n > 0 {
			changes["sharesRevoked"] = n
		}
	}
	if revoke {
		keep := ""
		if me := userFrom(r); me.ID == u.ID {
			if sess := sessionFrom(r); sess != nil {
				keep = sess.ID
			}
		}
		_ = s.db.DeleteUserSessions(ctx, u.ID, keep)
	}
	s.audit(r, userFrom(r), "user.update", changes)
	writeJSON(w, r, 200, map[string]any{"user": viewAdminUser(u)})
	return nil
}

func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) error {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	me := userFrom(r)
	if id == me.ID {
		return errorf(http.StatusConflict, "self", "cannot delete your own account")
	}
	ctx := r.Context()
	u, err := s.db.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if u.IsAdmin() && !u.Disabled {
		if n, _ := s.db.CountAdmins(ctx); n <= 1 {
			return errorf(http.StatusConflict, "last_admin", "cannot delete the last admin")
		}
	}
	// a lixeira do usuário some com ele (as linhas cascateiam; os arquivos não)
	if items, err := s.db.ListTrash(ctx, id); err == nil {
		for _, it := range items {
			_ = s.base.RemoveTrashItem(ctx, it.TrashDir, it.ID)
		}
	}
	// abort pending uploads so part files are removed
	if ups, err := s.db.ListUploads(ctx, id); err == nil {
		for _, up := range ups {
			_ = s.base.RemovePart(up.Dir, up.ID)
		}
	}
	if err := s.db.DeleteUser(ctx, id); err != nil {
		return err
	}
	s.audit(r, me, "user.delete", map[string]any{"id": id, "username": u.Username})
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}

// handleAdminDirs lists directories of the base root for the scope picker.
func (s *Server) handleAdminDirs(w http.ResponseWriter, r *http.Request) error {
	p, err := vfs.Normalize(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	l, err := s.base.List(p)
	if err != nil {
		return err
	}
	dirs := make([]string, 0)
	for _, e := range l.Entries {
		if e.Type == "dir" && !e.Link && !e.NameInvalid {
			dirs = append(dirs, e.Name)
		}
	}
	writeJSON(w, r, 200, map[string]any{"path": p, "dirs": dirs})
	return nil
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	entries, err := s.db.ListAudit(r.Context(), before, limit)
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"entries": entries})
	return nil
}
