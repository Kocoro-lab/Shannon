#!/usr/bin/env bash
#
# Validate all YAML templates in config/workflows/
#
set -euo pipefail

# Ensure bash 4+ for associative arrays
if [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
  echo "[WARN] Bash 4+ required for duplicate detection (found ${BASH_VERSION})"
  echo "[INFO] Skipping duplicate check..."
  SKIP_DUPLICATES=1
else
  SKIP_DUPLICATES=0
fi

TEMPLATE_DIR="${TEMPLATE_DIR:-config/workflows}"
FAILURES=0

info() { echo "[INFO] $1"; }
warn() { echo "[WARN] $1"; FAILURES=$((FAILURES + 1)); }
fail() { echo "[ERROR] $1"; FAILURES=$((FAILURES + 1)); }

info "Validating templates in $TEMPLATE_DIR"

# Check if directory exists
if [ ! -d "$TEMPLATE_DIR" ]; then
  warn "Template directory $TEMPLATE_DIR does not exist"
  exit 0  # Not a hard error - templates are optional
fi

# Find all YAML files
YAML_FILES=$(find "$TEMPLATE_DIR" -name "*.yaml" -o -name "*.yml" 2>/dev/null || echo "")

if [ -z "$YAML_FILES" ]; then
  info "No template files found"
  exit 0
fi

FILE_COUNT=$(echo "$YAML_FILES" | wc -l | tr -d ' ')
info "Found $FILE_COUNT template file(s)"

# Track template names and versions to detect duplicates
if [ "$SKIP_DUPLICATES" -eq 0 ]; then
  declare -A TEMPLATE_KEYS
fi

# Validate each file
while IFS= read -r FILE; do
  info "Validating: $FILE"

  # Check if file is readable
  if [ ! -r "$FILE" ]; then
    fail "$FILE: File not readable"
    continue
  fi

  # Basic YAML syntax check (using yq if available, otherwise python)
  if command -v yq >/dev/null 2>&1; then
    if ! yq eval . "$FILE" >/dev/null 2>&1; then
      fail "$FILE: Invalid YAML syntax"
      continue
    fi
  elif command -v python3 >/dev/null 2>&1; then
    if ! python3 -c "import yaml, sys; yaml.safe_load(open('$FILE'))" 2>/dev/null; then
      fail "$FILE: Invalid YAML syntax (python validation)"
      continue
    fi
  else
    warn "$FILE: Skipping syntax check (yq or python3 not available)"
  fi

  # Extract template name and version
  if command -v yq >/dev/null 2>&1; then
    NAME=$(yq eval '.name // ""' "$FILE" 2>/dev/null || echo "")
    VERSION=$(yq eval '.version // ""' "$FILE" 2>/dev/null || echo "")
  elif command -v python3 >/dev/null 2>&1; then
    NAME=$(python3 -c "import yaml; d=yaml.safe_load(open('$FILE')); print(d.get('name', ''))" 2>/dev/null || echo "")
    VERSION=$(python3 -c "import yaml; d=yaml.safe_load(open('$FILE')); print(d.get('version', ''))" 2>/dev/null || echo "")
  else
    # Fallback: grep-based extraction
    NAME=$(grep -E '^name:' "$FILE" | head -1 | sed 's/^name:[[:space:]]*//' | tr -d '"' || echo "")
    VERSION=$(grep -E '^version:' "$FILE" | head -1 | sed 's/^version:[[:space:]]*//' | tr -d '"' || echo "")
  fi

  # Validate required fields
  if [ -z "$NAME" ]; then
    fail "$FILE: Missing required field 'name'"
    continue
  fi

  # Check for duplicate name+version
  if [ "$SKIP_DUPLICATES" -eq 0 ]; then
    if [ -n "$VERSION" ]; then
      KEY="${NAME}@${VERSION}"
    else
      KEY="$NAME"
    fi

    if [ -n "${TEMPLATE_KEYS[$KEY]:-}" ]; then
      fail "Duplicate template: $KEY (found in $FILE and ${TEMPLATE_KEYS[$KEY]})"
    else
      TEMPLATE_KEYS[$KEY]="$FILE"
      info "  ✓ Template: $KEY"
    fi
  else
    # Just show the name without duplicate checking
    if [ -n "$VERSION" ]; then
      info "  ✓ Template: ${NAME}@${VERSION}"
    else
      info "  ✓ Template: $NAME"
    fi
  fi

done <<< "$YAML_FILES"

# Summary
echo ""
echo "================================"
if [ $FAILURES -eq 0 ]; then
  info "Template validation PASSED ($FILE_COUNT file(s))"
  exit 0
else
  fail "Template validation FAILED with $FAILURES error(s)"
  exit 1
fi
