package httpapi

import (
	"net/http"
	"strconv"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/store"
)

// --- Operator endpoints ---

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

// --- Agent endpoints ---

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
