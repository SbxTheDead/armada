package httpapi

import "strings"

// renderInstallScript produces the POSIX installer served at GET /manage.
//
// The script:
//  1. detects OS (uname -s) and CPU arch (uname -m) and maps them to the
//     matching agent build — the mapping mirrors goarchByAlias exactly;
//  2. downloads that binary from this server to /usr/local/bin/management-agent;
//  3. installs a service under whichever init system is present (systemd,
//     OpenRC, or a plain nohup fallback) with the server URL and enrollment
//     token baked in;
//  4. starts it. The agent renames itself to "MANAGEMENT AGENT" in htop/ps.
//
// serverURL and token are injected server-side. token may be empty, in which
// case the script expects ARMADA_ENROLL_TOKEN in the environment.
func renderInstallScript(serverURL, token, join string) string {
	r := strings.NewReplacer(
		"__SERVER__", shellQuote(serverURL),
		"__TOKEN__", shellQuote(token),
		"__JOIN__", shellQuote(join),
	)
	return r.Replace(installShTemplate)
}

func renderInstallPowerShell(serverURL, token, join string) string {
	r := strings.NewReplacer(
		"__SERVER__", psQuote(serverURL),
		"__TOKEN__", psQuote(token),
		"__JOIN__", psQuote(join),
	)
	return r.Replace(installPs1Template)
}

// shellQuote single-quotes a value for safe embedding in POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote single-quotes a value for PowerShell (doubling embedded quotes).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

const installShTemplate = `#!/bin/sh
# Armada management-agent installer. Auto-detects OS/arch, installs, and enrolls.
# Usage: curl -fsSL __SERVER__/manage?token=YOUR_TOKEN | sh
set -eu

SERVER=__SERVER__
TOKEN=__TOKEN__
JOIN=__JOIN__
[ -n "${TOKEN}" ] || TOKEN="${ARMADA_ENROLL_TOKEN:-}"
[ -n "${JOIN}" ]  || JOIN="${ARMADA_JOIN_TOKEN:-}"

BIN_NAME="management-agent"
BIN_PATH="/usr/local/bin/${BIN_NAME}"

log() { printf '\033[36m[armada]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[armada] error:\033[0m %s\n' "$*" >&2; exit 1; }

# Elevate if not root.
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then SUDO="sudo"; else die "run as root or install sudo"; fi
fi

# --- detect OS ---
case "$(uname -s)" in
  Linux)   GOOS=linux ;;
  Darwin)  GOOS=darwin ;;
  FreeBSD) GOOS=freebsd ;;
  OpenBSD) GOOS=openbsd ;;
  NetBSD)  GOOS=netbsd ;;
  *) die "unsupported OS: $(uname -s)" ;;
esac

# --- detect arch (mirrors the server's alias table) ---
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)                 GOARCH=amd64 ;;
  i386|i486|i586|i686|x86)      GOARCH=386 ;;
  aarch64|arm64)                GOARCH=arm64 ;;
  armv7*|armv6*|armhf|arm)      GOARCH=arm ;;
  riscv64)                      GOARCH=riscv64 ;;
  ppc64le|ppc64el)              GOARCH=ppc64le ;;
  s390x)                        GOARCH=s390x ;;
  mips64|mips64el|mips64le)     GOARCH=mips64le ;;
  mips|mipsel)                  GOARCH=mipsle ;;
  loongarch64|loong64)          GOARCH=loong64 ;;
  *) die "unsupported arch: $ARCH" ;;
esac

log "detected ${GOOS}/${GOARCH}; fetching agent from ${SERVER}"

# --- download ---
URL="${SERVER}/manage/bin/${GOOS}/${GOARCH}"
TMP="$(mktemp)"
if command -v curl >/dev/null 2>&1; then
  curl -fSL "$URL" -o "$TMP" || die "download failed from $URL"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$TMP" "$URL" || die "download failed from $URL"
else
  die "need curl or wget"
fi
$SUDO install -m 0755 "$TMP" "$BIN_PATH"
rm -f "$TMP"
log "installed ${BIN_PATH}"

if [ -z "${JOIN}" ] && [ -z "${TOKEN}" ]; then
  die "no credential; pass ?join=KEY (reusable) or ?token=... (single-use)"
fi

# --- install a service under whatever init system exists ---
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  log "configuring systemd service"
  $SUDO sh -c "cat > /etc/systemd/system/${BIN_NAME}.service" <<UNIT
[Unit]
Description=Armada Management Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=ARMADA_SERVER_URL=${SERVER}
Environment=ARMADA_JOIN_TOKEN=${JOIN}
Environment=ARMADA_ENROLL_TOKEN=${TOKEN}
Environment=ARMADA_AGENT_STATE=/var/lib/armada/agent.json
ExecStartPre=/bin/mkdir -p /var/lib/armada
ExecStart=${BIN_PATH}
Restart=always
RestartSec=5
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now ${BIN_NAME}.service
  log "started (systemctl status ${BIN_NAME})"

elif command -v rc-update >/dev/null 2>&1; then
  log "configuring OpenRC service"
  $SUDO sh -c "cat > /etc/init.d/${BIN_NAME}" <<'RC'
#!/sbin/openrc-run
name="management-agent"
command="/usr/local/bin/management-agent"
command_background=true
pidfile="/run/management-agent.pid"
output_log="/var/log/management-agent.log"
error_log="/var/log/management-agent.log"
depend() { need net; }
RC
  $SUDO sh -c "cat > /etc/conf.d/${BIN_NAME}" <<CONF
export ARMADA_SERVER_URL=${SERVER}
export ARMADA_JOIN_TOKEN=${JOIN}
export ARMADA_ENROLL_TOKEN=${TOKEN}
export ARMADA_AGENT_STATE=/var/lib/armada/agent.json
CONF
  $SUDO mkdir -p /var/lib/armada
  $SUDO chmod +x /etc/init.d/${BIN_NAME}
  $SUDO rc-update add ${BIN_NAME} default
  $SUDO rc-service ${BIN_NAME} restart
  log "started (rc-service ${BIN_NAME} status)"

else
  log "no systemd/OpenRC found; starting in the background via nohup"
  $SUDO mkdir -p /var/lib/armada
  ARMADA_SERVER_URL="${SERVER}" ARMADA_JOIN_TOKEN="${JOIN}" ARMADA_ENROLL_TOKEN="${TOKEN}" \
    ARMADA_AGENT_STATE=/var/lib/armada/agent.json \
    $SUDO -E nohup "${BIN_PATH}" >/var/log/management-agent.log 2>&1 &
  log "started (see /var/log/management-agent.log)"
fi

log "done — this device is now bound to the fleet."
`

const installPs1Template = `# Armada management-agent installer for Windows.
# Usage: iwr -useb __SERVER__/manage/install.ps1?join=YOUR_JOIN_KEY | iex
$ErrorActionPreference = 'Stop'

$Server = __SERVER__
$Token  = __TOKEN__
$Join   = __JOIN__
if ([string]::IsNullOrEmpty($Token)) { $Token = $env:ARMADA_ENROLL_TOKEN }
if ([string]::IsNullOrEmpty($Join))  { $Join  = $env:ARMADA_JOIN_TOKEN }
if ([string]::IsNullOrEmpty($Join) -and [string]::IsNullOrEmpty($Token)) {
  throw 'no credential; pass ?join=KEY (reusable) or ?token=... (single-use)'
}

switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { $Arch = 'amd64' }
  'ARM64' { $Arch = 'arm64' }
  'x86'   { $Arch = '386' }
  default { throw "unsupported arch: $($env:PROCESSOR_ARCHITECTURE)" }
}

$InstallDir = Join-Path $env:ProgramData 'Armada'
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$BinPath   = Join-Path $InstallDir 'management-agent.exe'
$StatePath = Join-Path $InstallDir 'agent.json'
$Wrapper   = Join-Path $InstallDir 'run.cmd'

Write-Host "[armada] downloading windows/$Arch agent from $Server"
Invoke-WebRequest -UseBasicParsing -Uri "$Server/manage/bin/windows/$Arch" -OutFile $BinPath

# A .cmd wrapper carries the config so the boot-time Scheduled Task has the
# right environment (a Task started at startup would otherwise inherit none).
@"
@echo off
set ARMADA_SERVER_URL=$Server
set ARMADA_JOIN_TOKEN=$Join
set ARMADA_ENROLL_TOKEN=$Token
set ARMADA_AGENT_STATE=$StatePath
"$BinPath"
"@ | Set-Content -Path $Wrapper -Encoding ASCII

# Register a startup Scheduled Task running the wrapper as SYSTEM, then start it.
$action    = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument "/c $Wrapper"
$trigger   = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName 'ArmadaManagementAgent' -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null
Start-ScheduledTask -TaskName 'ArmadaManagementAgent'
Write-Host '[armada] done — this device is now bound to the fleet.'
`
