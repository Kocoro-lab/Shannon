#!/usr/bin/env python3
"""
Local SkillSpector scan runner.
Usage: python scripts/scan_skills.py [core|user|vendor|all]
"""
import subprocess
import sys
from pathlib import Path

SKILLS_ROOT = Path("/home/Shannon/config/skills")
CATEGORIES = ["core", "user", "vendor"]

def run_scan(category: str) -> int:
    path = SKILLS_ROOT / category
    if not path.exists() or not any(path.iterdir()):
        print(f"[skip] {category}: directory not found or empty")
        return 0

    print(f"[scan] {category} -> {path}")
    result = subprocess.run(
        ["skillspector", "scan", str(path), "--format", "sarif", "--output", f"skillspector-{category}.sarif", "--fail-on", "HIGH"],
        capture_output=True,
        text=True
    )
    print(result.stdout)
    if result.stderr:
        print(result.stderr, file=sys.stderr)
    return result.returncode

def main():
    if len(sys.argv) < 2:
        print("Usage: python scripts/scan_skills.py [core|user|vendor|all]")
        sys.exit(1)

    target = sys.argv[1].lower()
    if target not in CATEGORIES + ["all"]:
        print(f"Invalid category: {target}. Choose from {CATEGORIES + ['all']}")
        sys.exit(1)

    categories = CATEGORIES if target == "all" else [target]
    exit_codes = []

    for cat in categories:
        code = run_scan(cat)
        exit_codes.append(code)

    if any(c != 0 for c in exit_codes):
        print("\n❌ SkillSpector found HIGH/CRITICAL issues")
        sys.exit(1)
    else:
        print("\n✅ All scans passed (no HIGH/CRITICAL findings)")
        sys.exit(0)

if __name__ == "__main__":
    main()
