#!/usr/bin/env bash
#
# build-native.sh — cross-compile a C module into per-architecture, statically
# linked binaries for Armada's native module runtime.
#
# It produces one binary per target under modules/dist/<name>/<os>-<arch>, which
# the control plane serves and each agent downloads for its own architecture.
#
# Usage:
#   ./build-native.sh <name> src/<name>.c
#   ./build-native.sh ftp src/ftp.c
#
# Toolchains (install on a Linux build server; Debian/Ubuntu package names):
#   gcc                         (x86_64 host)
#   gcc-aarch64-linux-gnu       (arm64)
#   gcc-arm-linux-gnueabihf     (armv7)
#   gcc-mips-linux-gnu          (mips, big-endian)
#   gcc-mipsel-linux-gnu        (mipsel)
#   gcc-powerpc64le-linux-gnu   (ppc64le)
#   gcc-riscv64-linux-gnu       (riscv64)
#
#   sudo apt-get install -y gcc gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf \
#       gcc-mips-linux-gnu gcc-mipsel-linux-gnu gcc-powerpc64le-linux-gnu \
#       gcc-riscv64-linux-gnu
#
# Tip: for maximum portability across libc versions, build against musl
# (e.g. the toolchains from https://musl.cc) instead of glibc.
set -euo pipefail

NAME="${1:?usage: build-native.sh <name> <source.c>}"
SRC="${2:?usage: build-native.sh <name> <source.c>}"

OUT="dist/${NAME}"
mkdir -p "${OUT}"

# target:  <os>-<arch>  ->  compiler
#          (armada os-arch name)     (cross gcc)
build() {
	local osarch="$1" cc="$2"
	if ! command -v "${cc}" >/dev/null 2>&1; then
		echo "skip ${osarch}: ${cc} not installed"
		return
	fi
	echo "build ${osarch} with ${cc}"
	# -static: no shared-libc dependency on the target device.
	"${cc}" -O2 -static -o "${OUT}/${osarch}" "${SRC}"
}

build linux-amd64   gcc
build linux-arm64   aarch64-linux-gnu-gcc
build linux-arm     arm-linux-gnueabihf-gcc
build linux-mips    mips-linux-gnu-gcc
build linux-mipsle  mipsel-linux-gnu-gcc
build linux-ppc64le powerpc64le-linux-gnu-gcc
build linux-riscv64 riscv64-linux-gnu-gcc

echo
echo "done. published builds for '${NAME}':"
ls -1 "${OUT}"
echo
echo "run it fleet-wide:  armada run ${NAME} --all"
