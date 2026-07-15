# Armada modules

A **module** is a small program compiled to a single WebAssembly file that the
agent runs, sandboxed, on a device. Because it's WASM, one `.wasm` runs on every
CPU architecture and OS the fleet has — you compile once, it runs everywhere.

Modules are written in **C** (this SDK), but any language that compiles to
`wasm32-wasi` and can call the host ABI works (Rust, TinyGo, Zig).

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

## Publishing & running

1. Drop the `.wasm` into the server's module dir (`modules/dist/` by default).
2. `armada modules` lists what's available.
3. `armada run ftp --all` (or `--region eu`, `--tag db`, …) dispatches it; each
   agent downloads the module, runs it, and returns the exit code + output.
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
