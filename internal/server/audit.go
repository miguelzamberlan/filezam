package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/zamberlan/filezam/internal/store"
)

// audit records an audit event. Best effort; failures are logged.
func (s *Server) audit(r *http.Request, u *store.User, action string, detail map[string]any) {
	e := &store.AuditEntry{TS: time.Now().Unix(), Action: action, IP: ipFrom(r)}
	if u != nil {
		id := u.ID
		e.UserID = &id
		e.Username = u.Username
	}
	if detail != nil {
		b, _ := json.Marshal(detail)
		e.Detail = string(b)
	}
	if err := s.db.AddAudit(context.Background(), e); err != nil {
		s.log.Warn("audit write failed", "err", err)
	}
}
