package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"time"

	"github.com/zamberlan/filezam/internal/store"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
	ctxIP
)

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxUser).(*store.User)
	return u
}

func sessionFrom(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxSession).(*store.Session)
	return s
}

func ipFrom(r *http.Request) string {
	ip, _ := r.Context().Value(ctxIP).(string)
	return ip
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic", "err", rec, "path", logPath(r.URL.Path), "stack", string(debug.Stack()))
				writeError(w, r, errorf(http.StatusInternalServerError, "internal", "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) trusted(ip netip.Addr) bool {
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// realIP resolves the client IP honoring X-Forwarded-For only from trusted proxies.
func (s *Server) realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := host
		if addr, err := netip.ParseAddr(host); err == nil && s.trusted(addr) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				hops := strings.Split(xff, ",")
				for i := len(hops) - 1; i >= 0; i-- {
					h := strings.TrimSpace(hops[i])
					a, err := netip.ParseAddr(h)
					if err != nil {
						break
					}
					ip = a.String()
					if !s.trusted(a) {
						break
					}
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxIP, ip)))
	})
}

// isHTTPS reports whether the client connection is HTTPS (directly or via a trusted proxy).
func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil && s.trusted(addr) {
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return false
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if s.isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// logPath hides share tokens carried in URL paths so they never land in stdout/log shipping.
func logPath(p string) string {
	for _, prefix := range []string{"/api/public/", "/s/"} {
		if rest, ok := strings.CutPrefix(p, prefix); ok && rest != "" {
			_, tail, found := strings.Cut(rest, "/")
			if found {
				tail = "/" + tail
			}
			return prefix + "<token>" + tail
		}
	}
	return p
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if s.log.Enabled(r.Context(), slog.LevelDebug) || sw.status >= 400 {
			s.log.Log(r.Context(), slog.LevelDebug, "http", "method", r.Method, "path", logPath(r.URL.Path), "status", sw.status, "ms", time.Since(start).Milliseconds(), "ip", ipFrom(r))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// csrfOK enforces the custom header and fetch metadata on mutating requests.
func csrfOK(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if r.Header.Get("X-Filezam") == "" {
		return false
	}
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
		return sfs == "same-origin" || sfs == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	rest := origin
	if i := strings.Index(origin, "://"); i >= 0 {
		rest = origin[i+3:]
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return strings.EqualFold(rest, r.Host)
}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !csrfOK(r) {
			writeError(w, r, errCSRF)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireUser authenticates the session cookie and loads the user.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !csrfOK(r) {
			writeError(w, r, errCSRF)
			return
		}
		u, sess, err := s.authenticate(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxSession, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requirePasswordOK blocks users who must change their password.
func (s *Server) requirePasswordOK(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFrom(r); u != nil && u.MustChangePassword {
			writeError(w, r, errorf(http.StatusForbidden, "password_change_required", "you must change your password first"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFrom(r); u == nil || !u.IsAdmin() {
			writeError(w, r, errForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
