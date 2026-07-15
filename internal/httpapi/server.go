// Package httpapi is the REST transport adapter. It translates HTTP requests
// into service calls and domain errors into HTTP status codes. It contains no
// business logic — that lives in internal/service.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/service"
)

// Server wires the fleet service to an http.Handler.
type Server struct {
	fleet         *service.Fleet
	log           *slog.Logger
	operatorToken string // optional static operator bearer (scaffold auth)
	distDir       string // directory of cross-compiled agent binaries to serve
	moduleDir     string // directory of modules to serve
}

// Config configures the HTTP server.
type Config struct {
	Fleet         *service.Fleet
	Logger        *slog.Logger
	OperatorToken string
	AgentDistDir  string
	ModuleDir     string
}

// New builds a Server.
func New(cfg Config) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	distDir := cfg.AgentDistDir
	if distDir == "" {
		distDir = "bin/agents"
	}
	moduleDir := cfg.ModuleDir
	if moduleDir == "" {
		moduleDir = "modules/dist"
	}
	return &Server{
		fleet:         cfg.Fleet,
		log:           log,
		operatorToken: cfg.OperatorToken,
		distDir:       distDir,
		moduleDir:     moduleDir,
	}
}

// Handler returns the fully-wired http.Handler with all middleware applied.
//
// Route groups:
//   - /healthz, /readyz            unauthenticated liveness/readiness
//   - /api/v1/...                  operator API (X-Tenant-ID + operator token)
//   - /agent/v1/join               unauthenticated (join key in body) onboarding
//   - /agent/v1/...                agent API (bearer API key)
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health checks — no auth.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleHealthz)

	// Operator API — requires operator auth + tenant.
	op := s.requireOperator
	mux.Handle("GET /api/v1/systems", op(http.HandlerFunc(s.handleListSystems)))
	mux.Handle("GET /api/v1/systems/{id}", op(http.HandlerFunc(s.handleGetSystem)))
	mux.Handle("GET /api/v1/systems/{id}/inventory", op(http.HandlerFunc(s.handleGetInventory)))
	mux.Handle("GET /api/v1/systems/{id}/metrics", op(http.HandlerFunc(s.handleGetMetrics)))
	mux.Handle("POST /api/v1/systems/{id}/approve", op(http.HandlerFunc(s.handleApproveSystem)))

	// Operator: reusable join tokens (zero-touch onboarding).
	mux.Handle("POST /api/v1/join-tokens", op(http.HandlerFunc(s.handleCreateJoinToken)))
	mux.Handle("GET /api/v1/join-tokens", op(http.HandlerFunc(s.handleListJoinTokens)))
	mux.Handle("DELETE /api/v1/join-tokens/{id}", op(http.HandlerFunc(s.handleRevokeJoinToken)))

	// Operator: module execution (jobs) + module catalog.
	mux.Handle("GET /api/v1/modules", op(http.HandlerFunc(s.handleListModules)))
	mux.Handle("POST /api/v1/jobs", op(http.HandlerFunc(s.handleCreateJob)))
	mux.Handle("GET /api/v1/jobs", op(http.HandlerFunc(s.handleListJobs)))
	mux.Handle("GET /api/v1/jobs/{id}", op(http.HandlerFunc(s.handleGetJob)))

	// Self-hosting agent distribution: /manage installer + binary downloads.
	s.registerManageRoutes(mux)

	// Agent join — the reusable join key in the body authorizes; no bearer yet.
	mux.HandleFunc("POST /agent/v1/join", s.handleJoin)

	// Agent API — requires a valid agent bearer key.
	ag := s.requireAgent
	mux.Handle("POST /agent/v1/heartbeat", ag(http.HandlerFunc(s.handleHeartbeat)))
	mux.Handle("POST /agent/v1/inventory", ag(http.HandlerFunc(s.handleInventory)))
	mux.Handle("GET /agent/v1/tasks", ag(http.HandlerFunc(s.handleClaimTasks)))
	mux.Handle("POST /agent/v1/tasks/{id}/result", ag(http.HandlerFunc(s.handleTaskResult)))
	mux.Handle("GET /agent/v1/modules/{name}", ag(http.HandlerFunc(s.handleDownloadModule)))

	// Middleware chain (outermost first): recover -> log -> routes.
	return s.recoverer(s.logRequests(mux))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// writeDomainError maps a domain sentinel to the right HTTP status.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrJoinToken):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// decodeJSON reads and strictly decodes a JSON request body.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)) // 1 MiB cap
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
