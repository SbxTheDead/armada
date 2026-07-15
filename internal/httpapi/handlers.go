package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/store"
)

// --- Operator endpoints ---

type createSystemRequest struct {
	Name        string            `json:"name"`
	FQDN        string            `json:"fqdn"`
	Project     string            `json:"project,omitempty"`
	Region      string            `json:"region,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

func (s *Server) handleCreateSystem(w http.ResponseWriter, r *http.Request) {
	var req createSystemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sys, err := s.fleet.RegisterSystem(r.Context(), domain.NewSystemInput{
		TenantID:    tenantFrom(r.Context()),
		Name:        req.Name,
		FQDN:        req.FQDN,
		Project:     req.Project,
		Region:      req.Region,
		Environment: req.Environment,
		Provider:    req.Provider,
		Tags:        req.Tags,
		Labels:      req.Labels,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sys)
}

func (s *Server) handleListSystems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.SystemFilter{
		Project:     q.Get("project"),
		Region:      q.Get("region"),
		Environment: q.Get("environment"),
		Provider:    q.Get("provider"),
		Tag:         q.Get("tag"),
		Lifecycle:   domain.Lifecycle(q.Get("lifecycle")),
		Health:      domain.Health(q.Get("health")),
		Limit:       atoiDefault(q.Get("limit"), 100),
		Offset:      atoiDefault(q.Get("offset"), 0),
	}
	systems, err := s.fleet.ListSystems(r.Context(), tenantFrom(r.Context()), filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"systems": systems,
		"count":   len(systems),
	})
}

func (s *Server) handleGetSystem(w http.ResponseWriter, r *http.Request) {
	sys, err := s.fleet.GetSystem(r.Context(), tenantFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sys)
}

func (s *Server) handleGetInventory(w http.ResponseWriter, r *http.Request) {
	inv, err := s.fleet.GetInventory(r.Context(), tenantFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	hb, err := s.fleet.LatestMetrics(r.Context(), tenantFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hb)
}

type issueTokenRequest struct {
	TTLSeconds int `json:"ttl_seconds,omitempty"`
}

type issueTokenResponse struct {
	Token     string    `json:"token"` // shown once
	SystemID  string    `json:"system_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var req issueTokenRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	tenant := tenantFrom(r.Context())
	systemID := r.PathValue("id")
	// Confirm the system exists in this tenant before minting a token for it.
	if _, err := s.fleet.GetSystem(r.Context(), tenant, systemID); err != nil {
		writeDomainError(w, err)
		return
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	plaintext, tok, err := s.fleet.IssueEnrollmentToken(r.Context(), tenant, systemID, ttl)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueTokenResponse{
		Token:     plaintext,
		SystemID:  systemID,
		ExpiresAt: tok.ExpiresAt,
	})
}

// --- Agent endpoints ---

type enrollRequest struct {
	Token string `json:"token"`
	FQDN  string `json:"fqdn,omitempty"`
}

type enrollResponse struct {
	SystemID string `json:"system_id"`
	APIKey   string `json:"api_key"` // shown once
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	res, err := s.fleet.Enroll(r.Context(), req.Token, req.FQDN)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollResponse{
		SystemID: res.System.ID,
		APIKey:   res.APIKey,
	})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := agentFrom(r.Context())
	var hb domain.Heartbeat
	if err := decodeJSON(r, &hb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// The agent can only report for the system it is bound to; ignore any
	// client-supplied system_id and force the authenticated identity.
	hb.SystemID = id.SystemID
	if err := s.fleet.RecordHeartbeat(r.Context(), id.TenantID, &hb); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	id := agentFrom(r.Context())
	var inv domain.Inventory
	if err := decodeJSON(r, &inv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	inv.SystemID = id.SystemID
	if err := s.fleet.RecordInventory(r.Context(), id.TenantID, &inv); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}
