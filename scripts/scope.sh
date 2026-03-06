#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq git find; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

# Read JSON input from stdin (or use defaults)
INPUT="{}"
if [ ! -t 0 ]; then
  INPUT="$(cat)"
  if [ -z "$INPUT" ]; then
    INPUT="{}"
  fi
fi

INCLUDE="$(echo "$INPUT" | jq -r '.include // [] | .[]' 2>/dev/null)"
EXCLUDE="$(echo "$INPUT" | jq -r '.exclude // [] | .[]' 2>/dev/null)"
MODE="$(echo "$INPUT" | jq -r '.mode // "tracked"')"
LIMIT="$(echo "$INPUT" | jq -r '.limit // 5000')"

# Collect files based on mode
collect_files() {
  if [ "$MODE" = "tracked" ]; then
    git ls-files 2>/dev/null
  else
    find . -type f \
      -not -path '*/.git/*' \
      -not -path '*/node_modules/*' \
      -not -path '*/vendor/*' \
      2>/dev/null | sed 's|^\./||'
  fi
}

# Apply include filters (if any)
apply_includes() {
  if [ -z "$INCLUDE" ]; then
    cat
  else
    local pattern_file
    pattern_file="$(mktemp)"
    # Convert glob patterns to grep-compatible patterns
    while IFS= read -r pattern; do
      # Convert glob to basic regex: ** -> .*, * -> [^/]*, ? -> .
      local regex
      regex="$(echo "$pattern" | sed -e 's|\.|\\.|g' -e 's|\*\*|__DOUBLESTAR__|g' -e 's|\*|[^/]*|g' -e 's|__DOUBLESTAR__|.*|g' -e 's|?|.|g')"
      echo "^${regex}$" >> "$pattern_file"
    done <<< "$INCLUDE"
    grep -E -f "$pattern_file" || true
    rm -f "$pattern_file"
  fi
}

# Apply exclude filters (if any)
apply_excludes() {
  if [ -z "$EXCLUDE" ]; then
    cat
  else
    local pattern_file
    pattern_file="$(mktemp)"
    while IFS= read -r pattern; do
      local regex
      regex="$(echo "$pattern" | sed -e 's|\.|\\.|g' -e 's|\*\*|__DOUBLESTAR__|g' -e 's|\*|[^/]*|g' -e 's|__DOUBLESTAR__|.*|g' -e 's|?|.|g')"
      echo "^${regex}$" >> "$pattern_file"
    done <<< "$EXCLUDE"
    grep -v -E -f "$pattern_file" || true
    rm -f "$pattern_file"
  fi
}

# Pipeline: collect -> include -> exclude -> limit -> JSONL
collect_files \
  | apply_includes \
  | apply_excludes \
  | head -n "$LIMIT" \
  | while IFS= read -r filepath; do
      [ -z "$filepath" ] && continue
      jq -cn --arg path "$filepath" '{"path": $path}'
    done
