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

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/store"
)

// --- Operator: module catalog ---

// moduleExt maps a served file extension to the runtime that executes it.
var moduleExt = map[string]domain.Runtime{
	".py": domain.RuntimePython,
}

type moduleInfo struct {
	Name    string         `json:"name"`
	Runtime domain.Runtime `json:"runtime"`
	Size    int64          `json:"size,omitempty"`
	SHA256  string         `json:"sha256,omitempty"`
	Targets []string       `json:"targets,omitempty"` // native: available os-arch builds
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
		if e.IsDir() {
			// A subdirectory is a native module: one binary per os-arch.
			targets := s.nativeTargets(e.Name())
			if len(targets) == 0 {
				continue
			}
			mods = append(mods, moduleInfo{
				Name:    e.Name(),
				Runtime: domain.RuntimeNative,
				Targets: targets,
			})
			continue
		}
		rt, ok := moduleExt[strings.ToLower(filepath.Ext(e.Name()))]
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mods = append(mods, moduleInfo{
			Name:    strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			Runtime: rt,
			Size:    info.Size(),
			SHA256:  fileSHA256(filepath.Join(s.moduleDir, e.Name())),
		})
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Name < mods[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"modules": mods, "count": len(mods)})
}

// moduleRuntime resolves a module name to its runtime: a <name>.py file, or a
// <name>/ directory of native per-arch binaries.
func (s *Server) moduleRuntime(name string) (domain.Runtime, bool) {
	if fi, err := os.Stat(filepath.Join(s.moduleDir, name+".py")); err == nil && !fi.IsDir() {
		return domain.RuntimePython, true
	}
	if fi, err := os.Stat(filepath.Join(s.moduleDir, name)); err == nil && fi.IsDir() {
		if len(s.nativeTargets(name)) > 0 {
			return domain.RuntimeNative, true
		}
	}
	return "", false
}

// nativeTargets lists the "os-arch" targets a native module directory provides.
func (s *Server) nativeTargets(name string) []string {
	entries, err := os.ReadDir(filepath.Join(s.moduleDir, name))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// File names are "<os>-<arch>" or "<os>-<arch>.exe".
		t := strings.TrimSuffix(e.Name(), ".exe")
		if strings.Count(t, "-") >= 1 {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
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
	// Resolve the module to its runtime (and confirm it exists) before dispatch.
	runtime, ok := s.moduleRuntime(req.Module)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown module %q; run 'armada modules' to list published modules", req.Module))
		return
	}
	filter := store.SystemFilter{
		Project:     req.Project,
		Region:      req.Region,
		Environment: req.Environment,
		Provider:    req.Provider,
		Tag:         req.Tag,
	}
	job, err := s.fleet.RunModule(r.Context(), tenantFrom(r.Context()), req.Module, runtime, req.Args, filter)
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
	runtime, ok := s.moduleRuntime(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("module %q not found", name))
		return
	}

	// Resolve the concrete file to serve.
	var relPath, contentType string
	switch runtime {
	case domain.RuntimePython:
		relPath, contentType = name+".py", "text/x-python"
	case domain.RuntimeNative:
		// The agent asks for the build matching its own OS/CPU.
		goos := clean(r.URL.Query().Get("os"))
		goarch := clean(r.URL.Query().Get("arch"))
		if goos == "" || goarch == "" {
			writeError(w, http.StatusBadRequest, "native module requires os and arch query params")
			return
		}
		file := goos + "-" + goarch
		if goos == "windows" {
			file += ".exe"
		}
		relPath = filepath.Join(name, file)
		contentType = "application/octet-stream"
		if _, err := os.Stat(filepath.Join(s.moduleDir, relPath)); err != nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("module %q has no build for %s/%s (available: %v)", name, goos, goarch, s.nativeTargets(name)))
			return
		}
	}

	f, err := os.Open(filepath.Join(s.moduleDir, relPath))
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
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Armada-Runtime", string(runtime))
	http.ServeContent(w, r, filepath.Base(relPath), info.ModTime(), f)
}

// clean restricts a query value to a safe path segment (no separators/dots).
func clean(s string) string {
	if strings.ContainsAny(s, `/\.`) {
		return ""
	}
	return s
}

func fileSHA256(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
