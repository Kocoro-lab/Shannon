#!/usr/bin/env python3
"""Executor script for Firecracker Python sandbox.

This script runs inside an isolated Firecracker microVM and executes
user-provided Python code. The exec() call here is intentional and safe
because the entire VM is isolated from the host system.
"""

import sys


def main():
    """Read Python code from stdin and execute it."""
    # Read code from stdin
    code = sys.stdin.read()

    # Execute code in isolated namespace
    # Note: This runs inside a Firecracker microVM, providing hardware-level isolation
    namespace = {"__name__": "__main__"}
    try:
        compiled = compile(code, "<stdin>", "exec")
        exec(compiled, namespace)  # noqa: S102 - Safe inside isolated VM
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
