package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// This file implements the self-hosting agent distribution: a device runs one
// command against the control plane and the correct agent is detected,
// downloaded, installed, and enrolled automatically.
//
//	GET /manage                       auto-detecting POSIX installer (curl | sh)
//	GET /manage/install.ps1           auto-detecting Windows installer (iwr | iex)
//	GET /manage/bin/{os}/{arch}       raw agent binary for an explicit target
//	GET /manage/{alias}               raw Linux agent binary by arch alias
//	                                  (e.g. /manage/x86, /manage/arm64)
//
// os/arch are validated against a fixed allowlist, so the file path served can
// never be attacker-controlled (no traversal).

// goarchByAlias maps the many names a device might report (or an operator might
// type) to a canonical GOARCH we build for. This is the "100% accurate"
// detection table shared by the alias route and the shell installer.
var goarchByAlias = map[string]string{
	"x86": "386", "i386": "386", "i486": "386", "i586": "386", "i686": "386", "386": "386",
	"x86_64": "amd64", "x64": "amd64", "amd64": "amd64",
	"aarch64": "arm64", "arm64": "arm64",
	"armv7": "arm", "armv7l": "arm", "armhf": "arm", "armv6": "arm", "armv6l": "arm", "arm": "arm",
	"riscv64": "riscv64",
	"ppc64le": "ppc64le", "ppc64el": "ppc64le",
	"s390x":    "s390x",
	"mips64le": "mips64le", "mips64el": "mips64le", "mips64": "mips64le",
	"mipsle": "mipsle", "mipsel": "mipsle",
	"loong64": "loong64", "loongarch64": "loong64",
}

// validTargets is the set of (os, arch) pairs the server will serve. A request
// outside this set is rejected before any filesystem access.
var validTargets = map[string]bool{
	"linux/amd64": true, "linux/arm64": true, "linux/arm": true, "linux/386": true,
	"linux/riscv64": true, "linux/ppc64le": true, "linux/s390x": true,
	"linux/mips64le": true, "linux/mipsle": true, "linux/loong64": true,
	"darwin/amd64": true, "darwin/arm64": true,
	"windows/amd64": true, "windows/arm64": true, "windows/386": true,
	"freebsd/amd64": true, "openbsd/amd64": true, "netbsd/amd64": true,
}

func (s *Server) registerManageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /manage", s.handleInstallScript)
	mux.HandleFunc("GET /manage/install.sh", s.handleInstallScript)
	mux.HandleFunc("GET /manage/install.ps1", s.handleInstallPowerShell)
	mux.HandleFunc("GET /manage/bin/{os}/{arch}", s.handleAgentBinary)
	mux.HandleFunc("GET /manage/{alias}", s.handleAgentBinaryByAlias)
}

// externalBaseURL reconstructs the URL the device used to reach us, honouring a
// reverse proxy's forwarded headers, so the installer bakes in a reachable
// server address.
func externalBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	script := renderInstallScript(externalBaseURL(r), q.Get("token"), q.Get("join"))
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func (s *Server) handleInstallPowerShell(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	script := renderInstallPowerShell(externalBaseURL(r), q.Get("token"), q.Get("join"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

// handleAgentBinary serves an explicit os/arch target.
func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	s.serveBinary(w, r, r.PathValue("os"), r.PathValue("arch"))
}

// handleAgentBinaryByAlias serves a Linux binary chosen by an arch alias, so
// /manage/x86, /manage/arm64, /manage/riscv64, etc. all work. It also accepts
// the two installer filenames so those routes resolve here without a conflict.
func (s *Server) handleAgentBinaryByAlias(w http.ResponseWriter, r *http.Request) {
	alias := strings.ToLower(r.PathValue("alias"))
	switch alias {
	case "install.sh":
		s.handleInstallScript(w, r)
		return
	case "install.ps1":
		s.handleInstallPowerShell(w, r)
		return
	}
	arch, ok := goarchByAlias[alias]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown arch alias %q; try /manage/bin/{os}/{arch} or /manage for the auto-installer", alias))
		return
	}
	s.serveBinary(w, r, "linux", arch)
}

func (s *Server) serveBinary(w http.ResponseWriter, r *http.Request, goos, goarch string) {
	target := goos + "/" + goarch
	if !validTargets[target] {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no agent build for %s", target))
		return
	}
	name := fmt.Sprintf("armada-agent-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	path := filepath.Join(s.distDir, name)
	f, err := os.Open(path)
	if err != nil {
		s.log.Warn("agent binary not available", "target", target, "path", path, "err", err)
		writeError(w, http.StatusServiceUnavailable,
			fmt.Sprintf("agent binary for %s is not built on this server; run 'make agents'", target))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stat binary")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	http.ServeContent(w, r, name, info.ModTime(), f)
}
