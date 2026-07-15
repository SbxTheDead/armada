# Armada modules

A **module** is a small program the agent runs on a device to do work. Two
runtimes are supported, chosen automatically from what you publish:

| Published as                  | Runtime | Runs how                                             |
| ----------------------------- | ------- | ---------------------------------------------------- |
| `<name>/<os>-<arch>` binaries | native  | native binary matching the device's CPU (no sandbox) |
| `<name>.py`                   | python  | the device's Python interpreter (no sandbox)         |

`armada run <name>` picks the runtime from what's published; you don't specify
it. `armada modules` shows each module's runtime.

## Layout

```
modules/
  src/              C sources for native modules (e.g. ftp.c)
  py/               Python module scripts (e.g. hello.py)
  dist/             what the control plane serves:
    <name>/<os>-<arch>   native builds
    <name>.py            python modules
  build-native.sh   cross-compile a C source into dist/<name>/<os>-<arch>
```

The control plane serves everything in `dist/` (configurable via
`ARMADA_MODULE_DIR`).

## Native modules (C) — the C path

Write normal C — it runs as a real native binary with full access, so it can use
the device shell directly via `system()`. Cross-compile it to a statically
linked binary per architecture; the agent downloads the build matching its own
CPU and runs it.

```c
// modules/src/ftp.c
#include <stdlib.h>
int main(void) {
    if (system("command -v apt-get >/dev/null 2>&1") == 0)
        return system("apt-get install -y vsftpd && systemctl enable --now vsftpd");
    return system("apk add vsftpd");
}
```

Your module's `main()` return value is the task exit code (`0` = success);
everything it prints is captured and returned to the operator. Arguments after
the module name arrive as `argv[1:]`.

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

Build them with the cross-compile script (install the toolchains it lists
first — see the header of the script):

```bash
cd modules
./build-native.sh ftp src/ftp.c    # -> dist/ftp/<os>-<arch>
armada run ftp --all
```

The agent requests `/agent/v1/modules/ftp?os=linux&arch=arm64`; if a device's
arch has no build, its task fails with the list of available targets. Tip:
build against **musl** (e.g. toolchains from https://musl.cc) for binaries that
run across libc versions.

## Python modules

A Python module is just a script — no compilation. It runs with the device's
Python interpreter (`python3`, or `py` on Windows; override with
`ARMADA_PYTHON`), with full access. Its exit code is the task result and its
output is captured. Arguments arrive as `sys.argv[1:]`.

```python
# modules/py/hello.py
import subprocess, sys
def main() -> int:
    subprocess.run(["apt-get", "install", "-y", "vsftpd"])  # or anything
    return 0
sys.exit(main())
```

Devices without Python fail the task with a clear "no Python interpreter found"
message. For universal reach on minimal devices, prefer a native module.

## Publishing & running

1. Put the artifact in the server's module dir (`modules/dist/` by default):
   a native module directory `<name>/<os>-<arch>`, or a `<name>.py` script.
2. `armada modules` lists what's available and each one's runtime.
3. `armada run ftp --all` (or `--region eu`, `--tag db`, …) dispatches it; each
   agent downloads the module, runs it with the matching runtime, and returns
   the exit code + captured output.
4. `armada jobs get <id>` shows per-device results.

> Modules run with the agent's privileges and are not sandboxed — signing,
> allowlisting, approval gates, and sandboxing are deferred to production
> hardening.
