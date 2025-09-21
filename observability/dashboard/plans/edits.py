#!/usr/bin/env python3
"""
Randomize timing fields in a plan file.

Defaults:
- estimate_ms: 10,000–30,000
- tps_min:     40–80
- tps_max:     80–120

Usage:
  python3 randomize_plan_fields.py path/to/plan.ts
  python3 randomize_plan_fields.py plan.ts --seed 42         # reproducible
  python3 randomize_plan_fields.py plan.ts --dry-run         # preview to stdout
  python3 randomize_plan_fields.py plan.ts --no-backup       # skip .bak
  # Ranges are overrideable:
  python3 randomize_plan_fields.py plan.ts --ms 9000 32000 --tps-min 30 70 --tps-max 70 140
"""
import argparse, random, re, shutil, sys

KEYS = ("estimate_ms", "tps_min", "tps_max")

def replace_field(text: str, field: str, lo: int, hi: int):
    # Match: estimate_ms: 12345  OR  "estimate_ms": 12345
    pat = re.compile(rf'(?P<prefix>(?:"{field}"|\'{field}\'|{field})\s*:\s*)\d+')
    def repl(m):
        return f"{m.group('prefix')}{random.randint(lo, hi)}"
    return pat.subn(repl, text)

def main():
    ap = argparse.ArgumentParser(description="Randomize estimate_ms, tps_min, tps_max in a plan file.")
    ap.add_argument("path", help="Path to .ts/.json-like plan file")
    ap.add_argument("--ms", dest="ms", nargs=2, type=int, default=(10_000, 30_000),
                    metavar=("LOW", "HIGH"), help="Range for estimate_ms")
    ap.add_argument("--tps-min", dest="tpsmin", nargs=2, type=int, default=(40, 80),
                    metavar=("LOW", "HIGH"), help="Range for tps_min")
    ap.add_argument("--tps-max", dest="tpsmax", nargs=2, type=int, default=(80, 120),
                    metavar=("LOW", "HIGH"), help="Range for tps_max")
    ap.add_argument("--seed", type=int, help="PRNG seed for reproducible outputs")
    ap.add_argument("--dry-run", action="store_true", help="Print result to stdout (don’t write file)")
    ap.add_argument("--no-backup", action="store_true", help="Don’t create .bak backup before writing")
    args = ap.parse_args()

    if args.seed is not None:
        random.seed(args.seed)

    try:
        with open(args.path, "r", encoding="utf-8") as f:
            txt = f.read()
    except OSError as e:
        sys.exit(f"Error reading {args.path}: {e}")

    txt, c_ms   = replace_field(txt, "estimate_ms", args.ms[0],    args.ms[1])
    txt, c_min  = replace_field(txt, "tps_min",     args.tpsmin[0], args.tpsmin[1])
    txt, c_max  = replace_field(txt, "tps_max",     args.tpsmax[0], args.tpsmax[1])

    if args.dry_run:
        sys.stdout.write(txt)
        return

    if not args.no_backup:
        try:
            shutil.copy2(args.path, args.path + ".bak")
        except OSError as e:
            sys.exit(f"Error creating backup: {e}")

    try:
        with open(args.path, "w", encoding="utf-8") as f:
            f.write(txt)
    except OSError as e:
        sys.exit(f"Error writing {args.path}: {e}")

    print(f"Updated {args.path}  (estimate_ms:{c_ms}, tps_min:{c_min}, tps_max:{c_max})")

if __name__ == "__main__":
    main()
