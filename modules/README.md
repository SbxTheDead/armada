# Armada modules

A **module** is a small program the agent runs on a device to do work. Three
runtimes are supported, chosen automatically from what you publish:

| Published as                    | Runtime | Runs how                                              |
| ------------------------------- | ------- | ----------------------------------------------------- |
| `<name>/<os>-<arch>` binaries   | native  | native binary matching the device's CPU (no sandbox)  |
| `<name>.py`                     | python  | the device's Python interpreter (no sandbox)          |
| `<name>.wasm`                   | wasm    | sandboxed, in-agent via wazero — one file, all arches |

`armada run <name>` picks the runtime from what's published; you don't specify
it. `armada modules` shows each module's runtime.

## Native modules (C) — recommended for C

Written in **C** (or anything gcc/clang compiles) and cross-compiled to a
statically-linked binary per architecture. The control plane serves the build
matching each device's CPU; the agent downloads and runs it directly — full
native speed and access, no sandbox, no interpreter.

Publish under a per-module directory, one file per target:

```
modules/dist/ftp/
  linux-amd64
  linux-arm64
  linux-arm
  linux-mips
  linux-riscv64
  windows-amd64.exe
```

Build them with the included cross-compile script (install the toolchains it
lists first):

```bash
cd modules
./build-native.sh ftp src/ftp.c    # -> dist/ftp/<os>-<arch>
armada run ftp --all
```

The agent requests `/agent/v1/modules/ftp?os=linux&arch=arm64`; if a device's
arch has no build, its task fails with the list of available targets.

## WASM modules (C)

Written in **C** (this SDK) — or any language that compiles to `wasm32-wasi`
and can call the host ABI (Rust, TinyGo, Zig). Because it's WASM, one `.wasm`
runs on every CPU architecture and OS the fleet has — compile once, run
everywhere.

## Layout

```
modules/
  src/           module source (C) + the SDK header
    armada.h
    install_ftp.c
  dist/          compiled <name>.wasm files the control plane serves
```

The control plane serves everything in `dist/` (configurable via
`ARMADA_MODULE_DIR`). The filename minus `.wasm` is the module name:
`dist/ftp.wasm` → `armada run ftp`.

## The ABI

Your module's `main()` return value is the task exit code (`0` = success).
Everything it prints (stdout/stderr) plus every command it runs is captured and
returned to the operator. Two host functions are available via `armada.h`:

| Function | Does |
| --- | --- |
| `armada_exec(cmd)` | run a shell command on the device, returns its exit code |
| `armada_log(msg)`  | append a message to the task output |
| `armada_have(bin)` | 1 if `bin` exists on the device, else 0 |

## Building

Install the [WASI SDK](https://github.com/WebAssembly/wasi-sdk), then:

```bash
export WASI_SYSROOT=/opt/wasi-sdk/share/wasi-sysroot
clang --target=wasm32-wasi --sysroot="$WASI_SYSROOT" -O2 \
      -o dist/ftp.wasm src/install_ftp.c
```

Or with the SDK's bundled clang:

```bash
/opt/wasi-sdk/bin/clang -O2 -o dist/ftp.wasm src/install_ftp.c
```

## Python modules

A Python module is just a script — no compilation. It runs with the device's
Python interpreter (`python3`, or `py` on Windows; override with
`ARMADA_PYTHON`), with full access. Its exit code is the task result and its
output is captured. Arguments are passed as `sys.argv[1:]`.

```python
# modules/py/hello.py
import subprocess, sys
def main() -> int:
    subprocess.run(["apt-get", "install", "-y", "vsftpd"])  # or anything
    return 0
sys.exit(main())
```

Devices without Python fail the task with a clear "no Python interpreter found"
message (a future `python.wasm` runtime can remove that requirement). WASM
modules have no such dependency — prefer them for universal reach.

## Publishing & running

1. Drop the artifact into the server's module dir (`modules/dist/` by default):
   a compiled `<name>.wasm`, or a `<name>.py` script.
2. `armada modules` lists what's available and each one's runtime.
3. `armada run ftp --all` (or `--region eu`, `--tag db`, …) dispatches it; each
   agent downloads the module, runs it with the matching runtime, and returns
   the exit code + output.
4. `armada jobs get <id>` shows per-device results.

## Writing your own

Copy `install_ftp.c`, change the commands, and rebuild to a new `dist/<name>.wasm`:

```c
#include "armada.h"

int main(void) {
    armada_log("hello from my module\n");
    return armada_exec("uptime");   // runs on the device; output is captured
}
```

> Note: modules currently run with the agent's privileges and no sandbox on the
> host commands they invoke — signing, allowlisting, and command sandboxing are
> deferred to production hardening.
