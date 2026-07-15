package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SbxTheDead/armada/internal/store"
)

// --- Operator: module catalog ---

type moduleInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.moduleDir)
	if err != nil {
		// An absent directory just means no modules published yet.
		writeJSON(w, http.StatusOK, map[string]any{"modules": []moduleInfo{}, "count": 0})
		return
	}
	var mods []moduleInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".wasm")
		info, err := e.Info()
		if err != nil {
			continue
		}
		mods = append(mods, moduleInfo{Name: name, Size: info.Size(), SHA256: fileSHA256(filepath.Join(s.moduleDir, e.Name()))})
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Name < mods[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"modules": mods, "count": len(mods)})
}

// --- Operator: jobs ---

type createJobRequest struct {
	Module      string   `json:"module"`
	Args        []string `json:"args,omitempty"`
	Project     string   `json:"project,omitempty"`
	Region      string   `json:"region,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Tag         string   `json:"tag,omitempty"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	filter := store.SystemFilter{
		Project:     req.Project,
		Region:      req.Region,
		Environment: req.Environment,
		Provider:    req.Provider,
		Tag:         req.Tag,
	}
	job, err := s.fleet.RunModule(r.Context(), tenantFrom(r.Context()), req.Module, req.Args, filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.fleet.ListJobs(r.Context(), tenantFrom(r.Context()))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "count": len(jobs)})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, tasks, err := s.fleet.JobStatus(r.Context(), tenantFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "tasks": tasks})
}

// --- Agent: task poll + result ---

func (s *Server) handleClaimTasks(w http.ResponseWriter, r *http.Request) {
	id := agentFrom(r.Context())
	tasks, err := s.fleet.ClaimTasks(r.Context(), id.TenantID, id.SystemID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

type taskResultRequest struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (s *Server) handleTaskResult(w http.ResponseWriter, r *http.Request) {
	id := agentFrom(r.Context())
	var req taskResultRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Confirm the task belongs to this agent's system before accepting a result.
	task, err := s.fleet.GetTaskForAgent(r.Context(), id.TenantID, id.SystemID, r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if err := s.fleet.CompleteTask(r.Context(), id.TenantID, task.ID, req.ExitCode, req.Output, req.Error); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "recorded"})
}

// --- Agent: module download ---

func (s *Server) handleDownloadModule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Guard against path traversal: module names are single path segments.
	if name == "" || strings.ContainsAny(name, `/\.`) {
		writeError(w, http.StatusBadRequest, "invalid module name")
		return
	}
	path := filepath.Join(s.moduleDir, name+".wasm")
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("module %q not found", name))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stat module")
		return
	}
	w.Header().Set("Content-Type", "application/wasm")
	http.ServeContent(w, r, name+".wasm", info.ModTime(), f)
}

func fileSHA256(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
