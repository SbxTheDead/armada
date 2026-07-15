# Armada

A CLI-first fleet-management platform for infrastructure you own or are
explicitly authorized to administer: VPSes, dedicated servers, homelab nodes,
SBCs, edge devices, and lab machines.

There is **no web dashboard**. You manage and monitor the fleet entirely from
the terminal with the `armada` CLI, which runs anywhere that can reach the
control plane — including directly on your VPS.

The platform is deliberately transparent: the agent only **observes and
reports** (inventory, health, metrics) and runs **administrator-approved**
maintenance tasks. It contains no persistence, stealth, credential harvesting,
privilege escalation, or defense-evasion behaviour by design.

---

## Components

| Binary          | Runs on          | Role                                                              |
| --------------- | ---------------- | ---------------------------------------------------------------- |
| `armada-server` | your VPS         | Control plane. Stores fleet state, ingests agent telemetry.      |
| `armada`        | anywhere         | Operator CLI. Register, inspect, and live-monitor devices.       |
| `armada-agent`  | each managed box | Enrolls, then reports inventory + heartbeats; runs approved jobs. |

```
   operator terminal                VPS                        managed devices
  ┌────────────────┐        ┌──────────────────┐            ┌────────────────┐
  │  armada  (CLI) │──REST──▶│   armada-server  │◀──REST────│  armada-agent  │ × N
  └────────────────┘        │   (control plane)│            └────────────────┘
        ▲                    └──────────────────┘                   │
        │  X-Tenant-ID + operator token          bearer API key ────┘
        └──────────────── you ────────────────────────────────────
```

Both the CLI and the agents speak the same REST API. The CLI authenticates as
an **operator** (bearer operator token + `X-Tenant-ID`); each agent
authenticates with a per-device **API key** it receives once, at enrollment.

---

## Architecture

Clean/hexagonal layering — dependencies point inward, transport and storage are
swappable adapters:

```
cmd/                    entrypoints (armada-server, armada, armada-agent)
internal/
  domain/               entities + invariants (System, Heartbeat, Inventory…)
  service/              use cases (register, enroll, ingest, health) — the core
  store/                persistence ports (interfaces)
    memory/             in-memory adapter (default; used by tests)
  httpapi/              REST transport adapter (server side)
  opclient/             operator REST client (used by the CLI)
  agent/                agent run loop + control-plane client
    inventory/          read-only host inventory collector
  config/               env-driven configuration
migrations/             PostgreSQL schema
deploy/                 Dockerfile + docker-compose
```

`internal/service` knows nothing about HTTP or SQL. Swapping the in-memory store
for PostgreSQL, or adding a gRPC transport, touches only the adapter layer.

### Health model

Health is derived, not stored as ground truth: a system is `healthy` when it has
beaconed within 3 heartbeat intervals and self-reports OK, `degraded` when it
beacons but reports a problem, and `offline` once it misses that window. The CLI
recomputes it at read time, so a box that stops beaconing shows `offline` even
if nothing wrote to it.

---

## Quick start

Requires Go 1.26+.

```bash
# 1. Build the three binaries into ./bin
make build

# 2. Run the control plane (in-memory store; zero external deps)
ARMADA_OPERATOR_TOKEN=op-secret ./bin/armada-server

# 3. In another shell, point the CLI at it
export ARMADA_SERVER_URL=http://localhost:8080
export ARMADA_OPERATOR_TOKEN=op-secret
export ARMADA_TENANT_ID=acme

# 4. Register a device, then mint its one-time enrollment token
./bin/armada systems register --name web-1 --fqdn web1.acme.internal --region eu-west
./bin/armada enroll <system-id>

# 5. On the device, run the agent with that token
ARMADA_SERVER_URL=http://<vps>:8080 \
ARMADA_ENROLL_TOKEN=<token> \
./bin/armada-agent

# 6. Watch the fleet live
./bin/armada monitor
```

---

## Binding a device or VM (one command)

The control plane hosts the agents and installs them for you — no manual
copying or per-architecture fiddling.

### Zero-touch: one reusable join key for the whole fleet (recommended)

Generate a key once. It never expires and binds any number of devices, so you
can bake it into a cloud-init snippet, a Dockerfile, an IoT image, or an Ansible
role and every machine self-registers on first boot.

```bash
# once, on the operator side:
armada join-token create --name fleet --region eu-west --tag iot
```

That prints a reusable command. Run it on any device or VM:

```bash
# Linux / macOS / BSD:
wget -qO- 'http://<vps>:8080/manage?join=<key>' | sh

# Windows (PowerShell, as Administrator):
iwr -useb 'http://<vps>:8080/manage/install.ps1?join=<key>' | iex
```

The installer detects the host (`uname -s` / `uname -m`, or
`$PROCESSOR_ARCHITECTURE` on Windows), downloads the matching agent, installs it
as a service (**systemd**, **OpenRC**, or a `nohup` fallback on Linux; a
**Scheduled Task** on Windows), and **auto-registers** the device — no prior
`systems register`. It picks up the key's group presets (project/region/tags),
and appears in `armada monitor`.

Devices are deduplicated by a stable machine id (`/etc/machine-id`,
Windows `MachineGuid`), so re-installs, reboots, and IP changes never create a
duplicate. Set `ARMADA_MACHINE_ID` to override it on cloned images/containers.

Manage keys with `armada join-token list` and `armada join-token revoke <id>`.
Options: `--approval manual` (device lands `pending` until `armada systems
approve <id>`), `--max-uses N`, and `--ttl` if you *do* want it to expire.

### Single-device: one-time token

For tight, per-machine control (register first, then bind exactly one device):

```bash
armada install-command <system-id>   # prints a curl|sh with a single-use ?token=
```

The running agent presents itself to process viewers as **`MANAGEMENT AGENT`**
— that's the name you'll see in `htop`, `ps`, and `top` on Linux.

### Serving specific binaries

The server exposes every build directly, so you can also just grab a binary:

| Route                          | Serves                                   |
| ------------------------------ | ---------------------------------------- |
| `GET /manage`                  | auto-detecting POSIX installer script    |
| `GET /manage/install.ps1`      | auto-detecting Windows installer         |
| `GET /manage/{arch}`           | Linux binary by alias (`x86`, `x86_64`, `arm64`, `armv7`, `riscv64`, `ppc64le`, `s390x`, `mips64le`, `loong64`, …) |
| `GET /manage/bin/{os}/{arch}`  | exact target, e.g. `/manage/bin/darwin/arm64` |

The `armada-server` serves these from its agent-distribution directory
(`ARMADA_AGENT_DIST_DIR`, default `bin/agents`). Populate it with `make agents`;
the Docker image bakes every target in automatically.

---

## CLI reference

```
armada systems register --name N --fqdn F [--project --region --environment --provider --tag ...]
armada systems list     [--region --health --lifecycle --project --provider --tag --limit] [--json]
armada systems get      <id> [--json]
armada systems inventory <id>
armada systems approve  <id>                         # activate a manual-approval join
armada join-token create [--name --project --region --environment --provider --tag --approval auto|manual --max-uses N --ttl D]
armada join-token list   [--json]
armada join-token revoke <id>
armada enroll           <system-id> [--ttl 15m]
armada install-command  <system-id> [--ttl 30m]     # single-device one-liner
armada monitor          [--interval 5s] [--once] [--region --health ...]
armada version
```

Global config (env, overridable per command with `--server` / `--token` /
`--tenant`):

| Env                     | Purpose                                    |
| ----------------------- | ------------------------------------------ |
| `ARMADA_SERVER_URL`     | control-plane base URL                     |
| `ARMADA_OPERATOR_TOKEN` | operator bearer token                      |
| `ARMADA_TENANT_ID`      | tenant to operate within (**required**)    |

### `monitor`

Auto-refreshing terminal view of the whole fleet — health dot, name, CPU%, mem,
disk, uptime, last check-in, agent version. `--once` prints a single snapshot
(useful in scripts); otherwise it refreshes every `--interval` until Ctrl-C.

---

## REST API (backs the CLI)

Operator endpoints require `Authorization: Bearer <operator-token>` and
`X-Tenant-ID`:

| Method | Path                                  | Purpose                        |
| ------ | ------------------------------------- | ------------------------------ |
| POST   | `/api/v1/systems`                     | register a device              |
| GET    | `/api/v1/systems`                     | list / filter devices          |
| GET    | `/api/v1/systems/{id}`                | device detail (live health)    |
| GET    | `/api/v1/systems/{id}/inventory`      | latest inventory snapshot      |
| GET    | `/api/v1/systems/{id}/metrics`        | latest heartbeat metrics       |
| POST   | `/api/v1/systems/{id}/enroll-token`   | mint one-time enrollment token |

Agent endpoints:

| Method | Path                     | Auth                    | Purpose                 |
| ------ | ------------------------ | ----------------------- | ----------------------- |
| POST   | `/agent/v1/enroll`       | enrollment token (body) | exchange token → API key |
| POST   | `/agent/v1/heartbeat`    | agent bearer key        | liveness + metrics      |
| POST   | `/agent/v1/inventory`    | agent bearer key        | upload inventory        |

Unauthenticated: `GET /healthz`, `GET /readyz`.

### Security notes

- Enrollment tokens and agent API keys are stored only as SHA-256 hashes;
  plaintext is shown exactly once.
- Operator token and enrollment tokens are compared in constant time.
- Enrollment tokens are single-use and expire (default 15 min).
- Agents can only report for the system their key is bound to; a client-supplied
  `system_id` on heartbeat/inventory is ignored in favour of the authenticated
  identity.
- The scaffold's operator auth (static token + `X-Tenant-ID`) is the seam where
  full OAuth2/OIDC + RBAC plugs in without touching the service layer.

---

## Deployment

```bash
# Build a static, non-root server image and run it with Postgres + Redis:
cd deploy
ARMADA_OPERATOR_TOKEN=change-me docker compose up --build
```

The `armada` CLI is baked into the image:

```bash
docker compose exec server armada systems list
```

### Cross-compiling agents

The agent is pure Go and cross-compiles to the full architecture matrix
(x86/amd64, arm/arm64, riscv64, mips64le, ppc64le, s390x, …) across Linux,
Windows, macOS, and the BSDs:

```bash
make agents   # writes bin/agents/armada-agent-<os>-<arch>
```

---

## Build & test

```bash
make test     # unit + HTTP integration tests (in-memory, no external services)
make vet      # go vet
make cover    # coverage summary
```

The test suite exercises the full flow — register → issue token → enroll →
heartbeat → health transition → offline detection — both at the service layer
and end-to-end through the real HTTP router.

---

## Status & roadmap

Implemented and verified end-to-end today: control plane, operator CLI,
management agent, enrollment, heartbeat/health, inventory, live monitor,
self-hosting one-command install (`/manage`, auto OS/arch detection, systemd/
OpenRC/Scheduled-Task), `MANAGEMENT AGENT` process name, in-memory storage,
Docker packaging, cross-compilation across the architecture matrix.

Designed-for and next up (adapter-layer work, no core changes):

- PostgreSQL store adapter (schema already in `migrations/`) + Redis cache
- NATS for job dispatch; the job system (schedule/queue/retry/cancel)
- gRPC transport alongside REST; Go/Python SDKs
- OAuth2/OIDC + RBAC operator auth; audit log
- Prometheus metrics + OpenTelemetry tracing
- Approved-maintenance-task execution + file upload/download with resume
