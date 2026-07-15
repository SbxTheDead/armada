/*
 * armada.h — SDK for writing Armada management-agent modules in C.
 *
 * A module is compiled to a single WebAssembly file and runs, sandboxed, inside
 * the agent on every device — one .wasm works on x86, arm64, riscv64, etc.
 *
 * The module's entrypoint is the normal C main(). Its return value is the task
 * exit code (0 = success). Anything printed with printf, or passed to
 * armada_log(), plus the output of every armada_exec() command, is captured and
 * returned to the operator as the task output.
 *
 * Build (needs the WASI SDK: https://github.com/WebAssembly/wasi-sdk):
 *   clang --target=wasm32-wasi --sysroot="$WASI_SYSROOT" -O2 \
 *         -o ../dist/ftp.wasm install_ftp.c
 *
 * The output filename (minus .wasm) is the module name you run:
 *   armada run ftp --all
 */
#ifndef ARMADA_H
#define ARMADA_H

#include <string.h>

/* Host imports provided by the agent (module "armada"). */

/* Run a shell command on the device; returns its exit code. Its combined
 * stdout/stderr is appended to the task output. */
__attribute__((import_module("armada"), import_name("exec")))
extern int __armada_exec(const char *ptr, int len);

/* Append a message to the task output. */
__attribute__((import_module("armada"), import_name("log")))
extern void __armada_log(const char *ptr, int len);

/* Convenience wrappers. */
static inline int armada_exec(const char *cmd) {
	return __armada_exec(cmd, (int)strlen(cmd));
}

static inline void armada_log(const char *msg) {
	__armada_log(msg, (int)strlen(msg));
}

/* Returns 1 if the given command exists on the device, else 0. */
static inline int armada_have(const char *cmd) {
	char probe[256];
	int n = 0;
	const char *pre = "command -v ";
	while (pre[n]) { probe[n] = pre[n]; n++; }
	int i = 0;
	while (cmd[i] && n < 200) { probe[n++] = cmd[i++]; }
	const char *post = " >/dev/null 2>&1";
	int j = 0;
	while (post[j]) { probe[n++] = post[j++]; }
	probe[n] = 0;
	return armada_exec(probe) == 0;
}

#endif /* ARMADA_H */
