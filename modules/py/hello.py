#!/usr/bin/env python3
"""hello.py — example Armada Python module.

A Python module is just a script. It runs on the device with the device's
Python interpreter, with full access. Its exit code is the task result;
everything it prints is captured and returned to the operator. Arguments after
the module name are passed as sys.argv[1:].

Publish it by dropping it into the server's module dir as <name>.py, then:
    armada run hello --all
"""
import platform
import subprocess
import sys


def main() -> int:
    print(f"armada python module on {platform.system()} {platform.machine()}")
    print(f"python {sys.version.split()[0]}, args={sys.argv[1:]}")

    # Python modules can shell out directly (no host ABI needed).
    try:
        out = subprocess.run(
            ["hostname"], capture_output=True, text=True, timeout=10
        )
        print(f"hostname: {out.stdout.strip()}")
    except Exception as exc:  # noqa: BLE001
        print(f"hostname failed: {exc}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
