# Armada modules

A **module** is a small program the agent runs on a device to do work. Two
runtimes are supported, chosen automatically by the file extension:

| File        | Runtime | Runs how                                             |
| ----------- | ------- | ---------------------------------------------------- |
| `<name>.wasm` | WASM  | sandboxed, in-agent via wazero — one file, all arches |
| `<name>.py`   | Python | on the device's Python interpreter (no sandbox)      |

`armada run <name>` picks the runtime from whichever file is published; you
don't specify it. `armada modules` shows each module's runtime.

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
