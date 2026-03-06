#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq find wc; do
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

MAX_DEPTH="$(echo "$INPUT" | jq -r '.max_depth // 6')"
MAX_FILES="$(echo "$INPUT" | jq -r '.max_files // 20000')"

# Collect paths to scan
PATHS=()
while IFS= read -r p; do
  [ -z "$p" ] && continue
  PATHS+=("$p")
done < <(echo "$INPUT" | jq -r '.paths // ["."] | .[]' 2>/dev/null)

if [ "${#PATHS[@]}" -eq 0 ]; then
  PATHS=(".")
fi

EXCLUDE_DIRS=(".git" "node_modules" "vendor" ".clock")
FIND_EXCLUDES=()
for d in "${EXCLUDE_DIRS[@]}"; do
  FIND_EXCLUDES+=(-name "$d" -prune -o)
done

# --- Build directory outline ---
# Collect all files, respecting exclusions and max_depth
ALL_FILES="$(mktemp)"
trap 'rm -f "$ALL_FILES"' EXIT

for scan_path in "${PATHS[@]}"; do
  find "$scan_path" -maxdepth "$MAX_DEPTH" \
    "${FIND_EXCLUDES[@]}" \
    -type f -print 2>/dev/null
done | head -n "$MAX_FILES" > "$ALL_FILES"

TOTAL_FILES="$(wc -l < "$ALL_FILES" | tr -d ' ')"

# Build outline: count files per top-level directory
OUTLINE_JSON="[]"
declare -A DIR_COUNTS=()
while IFS= read -r filepath; do
  [ -z "$filepath" ] && continue
  dir="$(dirname "$filepath")"
  # Get first meaningful directory component
  top_dir="$(echo "$dir" | cut -d'/' -f1-2)"
  DIR_COUNTS["$top_dir"]=$(( ${DIR_COUNTS["$top_dir"]:-0} + 1 ))
done < "$ALL_FILES"

# Sort directories by file count and build JSON
OUTLINE_JSON="$(
  for dir in "${!DIR_COUNTS[@]}"; do
    jq -cn --arg path "$dir/" --argjson files "${DIR_COUNTS[$dir]}" \
      '{"path": $path, "files": $files}'
  done | jq -s 'sort_by(-.files)'
)"

# --- Identify key files ---
KEY_PATTERNS=("main.*" "index.*" "app.*" "server.*" "Makefile" "Dockerfile" "*.config.*" "go.mod" "package.json" "Cargo.toml" "pyproject.toml")
KEY_FILES_JSON="[]"

for pattern in "${KEY_PATTERNS[@]}"; do
  while IFS= read -r filepath; do
    [ -z "$filepath" ] && continue
    KEY_FILES_JSON="$(echo "$KEY_FILES_JSON" | jq --arg p "$filepath" '. + [$p]')"
  done < <(grep -E "(^|/)${pattern//\*/[^/]*}$" "$ALL_FILES" 2>/dev/null | head -20)
done

# Deduplicate key files
KEY_FILES_JSON="$(echo "$KEY_FILES_JSON" | jq 'unique')"

# --- Identify hot spots ---
HOT_SPOTS_JSON="[]"

# Large files (> 500 lines)
while IFS= read -r filepath; do
  [ -z "$filepath" ] && continue
  [ ! -f "$filepath" ] && continue
  lines="$(wc -l < "$filepath" 2>/dev/null | tr -d ' ')" || continue
  if [ "$lines" -gt 500 ]; then
    HOT_SPOTS_JSON="$(echo "$HOT_SPOTS_JSON" | jq \
      --arg path "$filepath" \
      --argjson value "$lines" \
      '. + [{"path": $path, "reason": "large file", "value": $value}]')"
  fi
done < "$ALL_FILES"

# Recently modified files (top 10)
RECENT_FILES="$(sort -t$'\t' -k1 "$ALL_FILES" | while IFS= read -r f; do
  [ -f "$f" ] && stat -f '%m %N' "$f" 2>/dev/null || true
done | sort -rn | head -10)"

while IFS= read -r line; do
  [ -z "$line" ] && continue
  ts="$(echo "$line" | cut -d' ' -f1)"
  fpath="$(echo "$line" | cut -d' ' -f2-)"
  [ -z "$fpath" ] && continue
  HOT_SPOTS_JSON="$(echo "$HOT_SPOTS_JSON" | jq \
    --arg path "$fpath" \
    --argjson value "$ts" \
    '. + [{"path": $path, "reason": "recently modified", "value": $value}]')"
done <<< "$RECENT_FILES"

# Deduplicate hot spots by path
HOT_SPOTS_JSON="$(echo "$HOT_SPOTS_JSON" | jq 'unique_by(.path)')"

# --- Final output ---
jq -n \
  --argjson outline "$OUTLINE_JSON" \
  --argjson key_files "$KEY_FILES_JSON" \
  --argjson hot_spots "$HOT_SPOTS_JSON" \
  --argjson total_files "$TOTAL_FILES" \
  '{
    "total_files": $total_files,
    "outline": $outline,
    "key_files": $key_files,
    "hot_spots": $hot_spots
  }'
