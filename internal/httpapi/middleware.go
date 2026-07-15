package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
)

type ctxKey int

const (
	ctxKeyAgent  ctxKey = iota // *domain.AgentIdentity for agent-authenticated routes
	ctxKeyTenant               // tenant ID for operator routes
)

// recoverer converts panics into 500s and logs them, so a single bad request
// can never take down the process.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic in handler", "err", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logRequests emits one structured line per request with method, path, status,
// and latency.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"bytes", sw.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", clientIP(r),
		)
	})
}

// requireOperator authenticates a dashboard/CLI caller and resolves their
// tenant. This scaffold trusts the X-Tenant-ID header alongside a static bearer
// token from ARMADA_OPERATOR_TOKEN; production replaces this with the full
// OAuth2/OIDC + RBAC flow described in docs/auth.md, keeping this signature.
func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.operatorToken != "" {
			if !hasBearer(r, s.operatorToken) {
				writeError(w, http.StatusUnauthorized, "invalid operator credentials")
				return
			}
		}
		tenant := r.Header.Get("X-Tenant-ID")
		if tenant == "" {
			writeError(w, http.StatusBadRequest, "X-Tenant-ID header is required")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyTenant, tenant)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAgent authenticates a management agent by its bearer API key and puts
// the resolved identity in the request context.
func (s *Server) requireAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := bearerToken(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "missing agent credentials")
			return
		}
		id, err := s.fleet.AuthenticateAgent(r.Context(), key)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent credentials")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyAgent, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func agentFrom(ctx context.Context) *domain.AgentIdentity {
	id, _ := ctx.Value(ctxKeyAgent).(*domain.AgentIdentity)
	return id
}

func tenantFrom(ctx context.Context) string {
	t, _ := ctx.Value(ctxKeyTenant).(string)
	return t
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

func hasBearer(r *http.Request, want string) bool {
	got := bearerToken(r)
	if got == "" {
		return false
	}
	// Constant-time compare to avoid leaking the operator token by timing.
	return subtleEqual(got, want)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}

// statusWriter captures the status code and byte count for logging.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
