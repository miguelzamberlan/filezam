package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/miguelzamberlan/filezam/internal/store"
)

// Links quebrados por mudanças feitas por fora. Excluir, mover ou renomear pelo app revoga o link
// na hora (dropShares); o que acontece por Samba ou SSH só se descobre olhando. A manutenção olha
// de hora em hora: o link cujo item sumiu, trocou de tipo ou foi substituído (inode diferente)
// ganha broken_since, volta ao normal se o item reaparecer, e é revogado depois da carência.
const (
	// shareBrokenGrace é quanto um link fica quebrado antes de ser revogado. Longo o bastante para
	// um disco que não montou num reinício, ou uma pasta movida e desfeita, não levar os links.
	shareBrokenGrace = 24 * time.Hour
	// Se mais da metade dos links (e pelo menos brokenMassMin) quebra de uma vez, é muito mais
	// provável um disco ausente do que pessoas apagando coisas: nada é revogado nessa rodada.
	brokenMassMin = 4
)

// shareBroken reports whether the item a link points at is no longer the one it was created on.
func (s *Server) shareBroken(sh *store.Share) bool {
	e, err := s.base.Stat(sh.Path)
	if err != nil {
		return true
	}
	if (sh.Kind == "file") != (e.Type == "file") || (sh.Kind != "file" && e.Type != "dir") {
		return true
	}
	if sh.Ino != 0 {
		if dev, ino, err := s.base.Identity(sh.Path); err != nil || dev != sh.Dev || ino != sh.Ino {
			return true
		}
	}
	return false
}

// sweepBrokenShares marks, clears and finally revokes links whose item is gone.
func (s *Server) sweepBrokenShares(ctx context.Context) {
	if _, err := s.base.Stat(""); err != nil {
		return // raiz inacessível: nenhuma conclusão sobre os links
	}
	shares, err := s.db.ListLiveShares(ctx)
	if err != nil {
		s.log.Warn("broken shares", "err", err)
		return
	}
	now := s.db.Now().Unix()
	var broken, expired []*store.Share
	for _, sh := range shares {
		if ctx.Err() != nil {
			return
		}
		switch isBroken := s.shareBroken(sh); {
		case isBroken && sh.BrokenSince == nil:
			if err := s.db.SetShareBroken(ctx, sh.ID, &now); err != nil {
				s.log.Warn("broken shares: mark", "id", sh.ID, "err", err)
			}
			broken = append(broken, sh)
		case isBroken:
			broken = append(broken, sh)
			if now-*sh.BrokenSince >= int64(shareBrokenGrace.Seconds()) {
				expired = append(expired, sh)
			}
		case sh.BrokenSince != nil:
			_ = s.db.SetShareBroken(ctx, sh.ID, nil) // o item voltou
		}
	}
	if len(broken) >= brokenMassMin && len(broken)*2 > len(shares) {
		s.log.Warn("broken shares: most links point at missing items at once; not revoking (disk not mounted?)", "broken", len(broken), "live", len(shares))
		return
	}
	for _, sh := range expired {
		if sh.Mode == "drop" {
			s.abortShareUploads(ctx, sh.ID)
		}
		if err := s.db.DropShare(ctx, sh); err != nil {
			s.log.Warn("broken shares: revoke", "id", sh.ID, "err", err)
			continue
		}
		s.log.Info("share revoked: item missing", "id", sh.ID, "path", sh.Path, "since", *sh.BrokenSince)
		s.notifyShareRevoked(ctx, sh)
		s.auditSystem("share.revoke.broken", map[string]any{"id": sh.ID, "path": sh.Path, "owner": sh.CreatedByName, "brokenSince": *sh.BrokenSince})
	}
}

// auditSystem records an action taken by the server itself (sem requisição nem usuário).
func (s *Server) auditSystem(action string, detail map[string]any) {
	e := &store.AuditEntry{TS: time.Now().Unix(), Action: action}
	if detail != nil {
		b, _ := json.Marshal(detail)
		e.Detail = string(b)
	}
	if err := s.db.AddAudit(context.Background(), e); err != nil {
		s.log.Warn("audit write failed", "err", err)
	}
}
