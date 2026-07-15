package httpapi

import (
	"net/http"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/service"
)

// --- Agent: zero-touch join (unauthenticated; the join key authorizes) ---

type joinRequest struct {
	JoinToken string `json:"join_token"`
	domain.DeviceFacts
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req joinRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.JoinToken == "" {
		writeError(w, http.StatusBadRequest, "join_token is required")
		return
	}
	res, err := s.fleet.JoinWithToken(r.Context(), req.JoinToken, req.DeviceFacts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollResponse{
		SystemID: res.System.ID,
		APIKey:   res.APIKey,
	})
}

// --- Operator: join-token management ---

type createJoinTokenRequest struct {
	Name        string   `json:"name,omitempty"`
	Project     string   `json:"project,omitempty"`
	Region      string   `json:"region,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Approval    string   `json:"approval,omitempty"`    // auto|manual
	MaxUses     int      `json:"max_uses,omitempty"`    // 0 = unlimited
	TTLSeconds  int      `json:"ttl_seconds,omitempty"` // 0 = never expires
}

func (s *Server) handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
	var req createJoinTokenRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	plaintext, tok, err := s.fleet.CreateJoinToken(r.Context(), service.NewJoinTokenInput{
		TenantID:    tenantFrom(r.Context()),
		Name:        req.Name,
		Project:     req.Project,
		Region:      req.Region,
		Environment: req.Environment,
		Provider:    req.Provider,
		Tags:        req.Tags,
		Approval:    domain.ApprovalPolicy(req.Approval),
		MaxUses:     req.MaxUses,
		TTL:         time.Duration(req.TTLSeconds) * time.Second,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      tok.ID,
		"token":   plaintext,
		"details": tok,
	})
}

func (s *Server) handleListJoinTokens(w http.ResponseWriter, r *http.Request) {
	toks, err := s.fleet.ListJoinTokens(r.Context(), tenantFrom(r.Context()))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"join_tokens": toks,
		"count":       len(toks),
	})
}

func (s *Server) handleRevokeJoinToken(w http.ResponseWriter, r *http.Request) {
	if err := s.fleet.RevokeJoinToken(r.Context(), tenantFrom(r.Context()), r.PathValue("id")); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleApproveSystem(w http.ResponseWriter, r *http.Request) {
	sys, err := s.fleet.ApproveSystem(r.Context(), tenantFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sys)
}
