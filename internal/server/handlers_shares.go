package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

type shareView struct {
	ID           int64  `json:"id"`
	Token        string `json:"token"` // vazio em links criados antes da migração 002
	Slug         string `json:"slug"`  // apelido escolhido pelo usuário; vazio = link só por token
	Mode         string `json:"mode"`  // "read" | "drop"
	Path         string `json:"path"`
	Name         string `json:"name"`
	CreatedBy    string `json:"createdBy"`
	Mine         bool   `json:"mine"`
	CreatedAt    int64  `json:"createdAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	Expired      bool   `json:"expired"`
	AccessCount  int64  `json:"accessCount"`
	LastAccessAt *int64 `json:"lastAccessAt"`
	Kind         string `json:"kind"`        // "dir" | "file"
	HasPassword  bool   `json:"hasPassword"` // nunca o hash
	Revoked      bool   `json:"revoked"`     // apelido ainda reservado ao dono, link fora do ar
	QuotaBytes   int64  `json:"quotaBytes"`  // modo drop: teto e consumo do link
	UsedBytes    int64  `json:"usedBytes"`
	FileCount    int64  `json:"fileCount"`
	MaxFileBytes int64  `json:"maxFileBytes"`
	MaxFiles     int64  `json:"maxFiles"`
}

func (s *Server) viewShare(sh *store.Share, u *store.User) shareView {
	p := sh.Path
	if rel, ok := scopeRel(u.Scope, sh.Path); ok {
		p = rel
	}
	v := shareView{ID: sh.ID, Token: sh.Token, Slug: sh.Slug, Mode: sh.Mode, Path: p, Name: sh.Name, CreatedBy: sh.CreatedByName, Mine: sh.CreatedBy == u.ID, CreatedAt: sh.CreatedAt,
		ExpiresAt: sh.ExpiresAt, Expired: sh.ExpiresAt <= time.Now().Unix() || sh.RevokedAt != nil, AccessCount: sh.AccessCount, LastAccessAt: sh.LastAccessAt, Kind: sh.Kind,
		HasPassword: sh.PasswordHash != "", Revoked: sh.RevokedAt != nil, QuotaBytes: sh.QuotaBytes, MaxFileBytes: sh.MaxFileBytes, MaxFiles: sh.MaxFiles}
	if sh.Mode == "drop" {
		if usage, err := s.db.ShareUsage(context.Background(), sh.ID); err == nil {
			v.UsedBytes, v.FileCount = usage.Bytes, usage.Count
		}
	}
	return v
}

// shareURL builds the public address. O apelido tem precedência sobre o token: é o endereço
// que o usuário escolheu para divulgar.
func (s *Server) shareURL(r *http.Request, token string) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL + "/s/" + token
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if fh := lastHeader(r, "X-Forwarded-Host"); fh != "" && s.isTrustedRequest(r) {
		host = fh
	}
	return scheme + "://" + host + "/s/" + token
}

// dropShares removes the links to base-relative path p and to everything below it (any
// owner). Excluir, mover ou renomear pelo app sempre derruba o link: o inode gravado só
// protege contra mudanças feitas por fora, e alguns sistemas de arquivos o reutilizam.
func (s *Server) dropShares(p string) {
	ctx := context.Background()
	// Sessões de envio em voo morrem junto com o link: sem isto a parte reservada ficaria no
	// disco até a varredura horária, num caminho que talvez nem exista mais.
	if shares, err := s.db.ListSharesUnder(ctx, p); err == nil {
		for _, sh := range shares {
			if sh.Mode == "drop" {
				s.abortShareUploads(ctx, sh.ID)
			}
		}
	}
	if n, err := s.db.DeleteSharesUnder(ctx, p); err != nil {
		s.log.Warn("drop shares", "path", p, "err", err)
	} else if n > 0 {
		s.log.Info("shares revoked with their item", "path", p, "count", n)
	}
}

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	uid := u.ID
	if u.IsAdmin() {
		uid = 0
	}
	shares, err := s.db.ListShares(r.Context(), uid)
	if err != nil {
		return err
	}
	out := make([]shareView, 0, len(shares))
	for _, sh := range shares {
		// Revogados normalmente somem; os que têm apelido continuam listados para que o dono
		// veja o endereço que segue reservado a ele e possa liberá-lo.
		if sh.RevokedAt != nil && sh.Slug == "" {
			continue
		}
		out = append(out, s.viewShare(sh, u))
	}
	writeJSON(w, r, 200, map[string]any{"shares": out, "now": time.Now().Unix()})
	return nil
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	var in struct {
		Path      string `json:"path"`
		ExpiresIn int64  `json:"expiresIn"`
		Name      string `json:"name"`
		Password  string `json:"password"` // opcional; 8–256 caracteres
		Slug      string `json:"slug"`     // apelido; exige senha
		Mode      string `json:"mode"`     // "read" (padrão) | "drop"
		// Só no modo drop. QuotaBytes é obrigatório; os outros dois caem no padrão.
		QuotaBytes   int64 `json:"quotaBytes"`
		MaxFileBytes int64 `json:"maxFileBytes"`
		MaxFiles     int64 `json:"maxFiles"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	set := s.settings()
	mode := in.Mode
	if mode == "" {
		mode = "read"
	}
	if mode != "read" && mode != "drop" {
		return errorf(http.StatusBadRequest, "bad_mode", "mode must be read or drop")
	}
	if mode == "drop" {
		if err := requireFeature(set.DropEnabled); err != nil {
			return err
		}
	}
	slug := normalizeSlug(in.Slug)
	if slug != "" {
		if err := requireFeature(set.SlugsEnabled); err != nil {
			return err
		}
		if err := checkSlug(slug); err != nil {
			return err
		}
		// O apelido é adivinhável por construção: o segredo do link passa a ser a senha.
		if in.Password == "" {
			return errPasswordRequired
		}
		if taken, err := s.db.SlugTaken(r.Context(), slug); err != nil {
			return err
		} else if taken {
			return errSlugTaken
		}
	}
	p, err := vfs.NormalizeWritable(in.Path) // nunca compartilhar lixeira ou partes de upload
	if err != nil {
		return err
	}
	kind := "dir"
	created := false // a pasta do link de envio nasce aqui; desfazemos se algo mais falhar
	if mode == "drop" {
		if p == "" {
			return errorf(http.StatusBadRequest, "root_op", "a drop link needs its own folder")
		}
		if created, err = s.prepareDropFolder(root, p); err != nil {
			return err
		}
		defer func() {
			if created && err != nil {
				_ = root.Remove(p)
			}
		}()
	} else {
		var e *vfs.Entry
		if e, err = root.Stat(p); err != nil {
			return err
		}
		if e.Type != "dir" && e.Type != "file" {
			return vfs.ErrNotDir
		}
		if p == "" && e.Type == "file" {
			return vfs.ErrNotDir
		}
		kind = e.Type
	}
	pwHash := ""
	if in.Password != "" {
		// O mínimo é o mesmo das senhas de conta. Num link com apelido ela é o único segredo (o
		// endereço é escolhido para ser fácil de dizer, logo fácil de adivinhar), e mesmo num link
		// por token uma senha de 4 caracteres dá a quem recebeu o endereço a ilusão de proteção.
		if n := len([]rune(in.Password)); n < auth.MinPasswordLen || n > 256 {
			err = errorf(http.StatusBadRequest, "weak_password", "share password must have %d-256 characters", auth.MinPasswordLen)
			return err
		}
		if pwHash, err = auth.HashPassword(in.Password); err != nil {
			return err
		}
	}
	maxTTL := int64(s.cfg.ShareMaxTTL.Seconds())
	if mode == "drop" {
		maxTTL = s.dropMaxTTL()
	}
	if in.ExpiresIn <= 0 || in.ExpiresIn > maxTTL {
		err = errorf(http.StatusBadRequest, "bad_expiry", "expiresIn must be between 1 and %d seconds", maxTTL)
		return err
	}
	var quota, fileMax, maxFiles int64
	if mode == "drop" {
		if quota, fileMax, maxFiles, err = s.dropLimits(r.Context(), u, in.QuotaBytes, in.MaxFileBytes, in.MaxFiles); err != nil {
			return err
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = vfs.Base(p)
		if name == "" {
			name = "Arquivos"
		}
	}
	name = truncName(name, 100)
	tok, err := auth.NewToken(32)
	if err != nil {
		return err
	}
	dev, ino, _ := root.Identity(p) // zeros quando o sistema de arquivos não informa
	now := time.Now().Unix()
	sh, err := s.db.CreateShare(r.Context(), &store.Share{TokenHash: auth.HashToken(tok), Token: tok, Slug: slug, Mode: mode, Kind: kind, PasswordHash: pwHash,
		Dev: dev, Ino: ino, QuotaBytes: quota, MaxFileBytes: fileMax, MaxFiles: maxFiles, Path: vfs.Join(u.Scope, p), Name: name, CreatedBy: u.ID, CreatedAt: now, ExpiresAt: now + in.ExpiresIn})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			err = errSlugTaken // corrida entre dois pedidos do mesmo apelido
		}
		return err
	}
	s.audit(r, u, "share.create", map[string]any{"id": sh.ID, "path": sh.Path, "kind": sh.Kind, "mode": mode, "slug": slug,
		"password": pwHash != "", "expiresAt": sh.ExpiresAt, "quotaBytes": quota, "maxFiles": maxFiles})
	addr := tok
	if slug != "" {
		addr = slug
	}
	writeJSON(w, r, 201, map[string]any{"share": s.viewShare(sh, u), "token": tok, "url": s.shareURL(r, addr)})
	return nil
}

// prepareDropFolder makes sure the drop link owns an empty folder, creating it when needed.
// Apontar um link de envio para uma pasta que já tem arquivos exporia o que está lá a qualquer
// corrida de nome e misturaria o que chega da internet com o que já era do usuário.
func (s *Server) prepareDropFolder(root *vfs.Root, p string) (created bool, err error) {
	switch e, serr := root.Stat(p); {
	case serr == nil:
		if e.Type != "dir" {
			return false, vfs.ErrNotDir
		}
		empty, err := root.IsEmpty(p)
		if err != nil {
			return false, err
		}
		if !empty {
			return false, errNotEmpty
		}
		return false, nil
	case errors.Is(serr, vfs.ErrNotFound):
		if err := root.MkdirAll(vfs.Dir(p)); err != nil {
			return false, err
		}
		if err := root.Mkdir(p); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, serr
	}
}

// dropLimits validates the caps of a new drop link against the administrator's ceilings and
// the owner's own disk quota.
func (s *Server) dropLimits(ctx context.Context, u *store.User, quota, fileMax, maxFiles int64) (int64, int64, int64, error) {
	set := s.settings()
	n, err := s.db.CountActiveDropLinks(ctx, u.ID)
	if err != nil {
		return 0, 0, 0, err
	}
	if int64(n) >= set.DropMaxLinks {
		return 0, 0, 0, errorf(http.StatusConflict, "drop_links_exceeded", "you already have %d active drop links", n)
	}
	if quota <= 0 || quota > set.DropMaxQuota {
		return 0, 0, 0, errorf(http.StatusBadRequest, "bad_quota", "quotaBytes must be between 1 and %d", set.DropMaxQuota)
	}
	// O link não pode prometer mais espaço do que o dono ainda tem.
	if u.Quota > 0 {
		used, err := s.usage(ctx, u)
		if err != nil {
			return 0, 0, 0, err
		}
		if free := u.Quota - used; quota > free {
			return 0, 0, 0, errorf(http.StatusBadRequest, "bad_quota", "quotaBytes must not exceed your remaining quota of %d bytes", max(free, 0))
		}
	}
	if fileMax <= 0 {
		fileMax = set.DropFileMax
	}
	if fileMax > quota {
		fileMax = quota
	}
	if maxFiles <= 0 {
		maxFiles = set.DropMaxFiles
	}
	if maxFiles > set.DropMaxFiles {
		return 0, 0, 0, errorf(http.StatusBadRequest, "bad_quota", "maxFiles must not exceed %d", set.DropMaxFiles)
	}
	return quota, fileMax, maxFiles, nil
}

// truncName cuts a name to n bytes without splitting a UTF-8 rune in half.
func truncName(name string, n int) string {
	if len(name) <= n {
		return name
	}
	for n > 0 && !utf8.RuneStart(name[n]) {
		n--
	}
	return name[:n]
}

func (s *Server) handleShareDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	u := userFrom(r)
	sh, err := s.db.GetShare(r.Context(), id)
	if err != nil {
		return err
	}
	if sh.CreatedBy != u.ID && !u.IsAdmin() {
		return errForbidden
	}
	if sh.Mode == "drop" {
		s.abortShareUploads(r.Context(), sh.ID)
	}
	// Sem ?purge=1, um link com apelido é revogado e não apagado: o endereço continua preso a
	// quem o criou, senão outra pessoa o reivindicaria e passaria a receber o que era dele.
	purge := queryBool(r, "purge") || sh.Slug == ""
	if purge {
		err = s.db.DeleteShare(r.Context(), id)
	} else {
		err = s.db.RevokeShare(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			err = nil // já estava revogado
		}
	}
	if err != nil {
		return err
	}
	s.audit(r, u, "share.revoke", map[string]any{"id": id, "path": sh.Path, "slug": sh.Slug, "purged": purge})
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
