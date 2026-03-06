#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq rg; do
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

# Extract focus paths (directories/files to prioritize)
FOCUS=()
while IFS= read -r f; do
  [ -z "$f" ] && continue
  FOCUS+=("$f")
done < <(echo "$INPUT" | jq -r '.focus // [] | .[]' 2>/dev/null)

# Determine scan paths from map data or default to "."
SCAN_PATHS=()
if [ "${#FOCUS[@]}" -gt 0 ]; then
  SCAN_PATHS=("${FOCUS[@]}")
else
  while IFS= read -r p; do
    [ -z "$p" ] && continue
    SCAN_PATHS+=("$p")
  done < <(echo "$INPUT" | jq -r '.map.outline // [] | .[].path // empty' 2>/dev/null)
fi

if [ "${#SCAN_PATHS[@]}" -eq 0 ]; then
  SCAN_PATHS=(".")
fi

# --- Side effects detection ---
SIDEFX="$(mktemp)"
trap 'rm -f "$SIDEFX"' EXIT

# File I/O patterns
SIDE_EFFECT_PATTERNS=(
  'fs\.write|fs\.writeFile|fs\.appendFile|fs\.createWriteStream'
  'fs\.read|fs\.readFile|fs\.createReadStream'
  'os\.Create|os\.OpenFile|os\.Remove|os\.Rename|os\.MkdirAll'
  'ioutil\.WriteFile|ioutil\.ReadFile'
  'open\(.*["\x27]w'
  'with\s+open\('
)

# HTTP / network patterns
SIDE_EFFECT_PATTERNS+=(
  'fetch\('
  'http\.(Get|Post|Put|Delete|Do|NewRequest)\('
  'axios\.'
  'requests\.(get|post|put|delete|patch)\('
  'net\.Dial|net\.Listen'
  'grpc\.'
)

# Database patterns
SIDE_EFFECT_PATTERNS+=(
  'sql\.(Open|Query|Exec|Prepare)'
  'db\.(Query|Exec|Prepare|Begin|Transaction)'
  'mongoose\.'
  'prisma\.'
  'sequelize\.'
  'INSERT\s+INTO|UPDATE\s+.*SET|DELETE\s+FROM'
)

# Process / exec patterns
SIDE_EFFECT_PATTERNS+=(
  'exec\.(Command|Run)\('
  'child_process'
  'spawn\('
  'subprocess\.'
  'os\.system\('
)

for pattern in "${SIDE_EFFECT_PATTERNS[@]}"; do
  for scan_path in "${SCAN_PATHS[@]}"; do
    rg --no-heading --line-number --with-filename \
      -e "$pattern" "$scan_path" 2>/dev/null || true
  done | while IFS= read -r line; do
    [ -z "$line" ] && continue
    fpath="$(echo "$line" | cut -d':' -f1)"
    linenum="$(echo "$line" | cut -d':' -f2)"
    text="$(echo "$line" | cut -d':' -f3- | sed -E 's/^[[:space:]]*//' | head -c 100)"
    echo "${text} in ${fpath}:${linenum}"
  done >> "$SIDEFX"
done

# Deduplicate and limit side effects
SIDEFX_JSON="$(sort -u "$SIDEFX" | head -200 | jq -R . | jq -s '.')"

# --- Import/dependency analysis for pipelines ---
EDGES_JSON="[]"
PIPELINES_JSON="[]"

# Find import/require relationships
IMPORTS_FILE="$(mktemp)"
trap 'rm -f "$SIDEFX" "$IMPORTS_FILE"' EXIT

for scan_path in "${SCAN_PATHS[@]}"; do
  # JS/TS imports
  rg --no-heading --line-number --with-filename \
    -e "import\s+.*from\s+['\"]" -e "require\(['\"]" \
    "$scan_path" 2>/dev/null || true

  # Go imports
  rg --no-heading --line-number --with-filename \
    -e '"[^"]+/[^"]*"' --glob '*.go' \
    "$scan_path" 2>/dev/null || true

  # Python imports
  rg --no-heading --line-number --with-filename \
    -e '^(from|import)\s+' --glob '*.py' \
    "$scan_path" 2>/dev/null || true
done > "$IMPORTS_FILE" 2>/dev/null || true

# Build edges from imports
while IFS= read -r line; do
  [ -z "$line" ] && continue
  fpath="$(echo "$line" | cut -d':' -f1)"
  text="$(echo "$line" | cut -d':' -f3-)"

  # Extract the imported module/path
  target=""
  # JS/TS: from 'xxx' or require('xxx')
  target="$(echo "$text" | grep -oE "(from\s+['\"])([^'\"]+)" | sed -E "s/from\s+['\"]//g" | head -1)" || true
  if [ -z "$target" ]; then
    target="$(echo "$text" | grep -oE "require\(['\"]([^'\"]+)" | sed -E "s/require\(['\"]//g" | head -1)" || true
  fi
  # Go: "package/path"
  if [ -z "$target" ]; then
    target="$(echo "$text" | grep -oE '"[^"]+/[^"]*"' | tr -d '"' | head -1)" || true
  fi
  # Python: from x import / import x
  if [ -z "$target" ]; then
    target="$(echo "$text" | grep -oE '(from|import)\s+([a-zA-Z0-9_.]+)' | sed -E 's/(from|import)\s+//' | head -1)" || true
  fi

  if [ -n "$target" ]; then
    EDGES_JSON="$(echo "$EDGES_JSON" | jq \
      --arg from "$fpath" \
      --arg to "$target" \
      '. + [{"from": $from, "to": $to, "kind": "imports"}]' 2>/dev/null || echo "$EDGES_JSON")"
  fi
done < <(head -500 "$IMPORTS_FILE")

# Deduplicate edges
EDGES_JSON="$(echo "$EDGES_JSON" | jq 'unique_by(.from + "->" + .to)' 2>/dev/null || echo '[]')"

# --- Infer pipelines from contracts data ---
# Extract pipeline hints from routes -> handlers -> services -> db
CTRT_ROUTES="$(echo "$INPUT" | jq '.ctrt.routes // []' 2>/dev/null || echo '[]')"
ROUTE_COUNT="$(echo "$CTRT_ROUTES" | jq 'length' 2>/dev/null || echo '0')"

if [ "$ROUTE_COUNT" -gt 0 ]; then
  # Group routes by file to infer pipeline groupings
  PIPELINES_JSON="$(echo "$CTRT_ROUTES" | jq '
    group_by(.path) |
    map({
      "name": (.[0].path | split("/") | .[-1] | split(".") | .[0]),
      "steps": [.[] | .name | split("(") | .[0] | ltrimstr(" ")]
    }) |
    map(select(.steps | length > 0))
  ' 2>/dev/null || echo '[]')"
fi

# Count side effects by type
SIDEFX_COUNT="$(echo "$SIDEFX_JSON" | jq 'length' 2>/dev/null || echo '0')"
EDGES_COUNT="$(echo "$EDGES_JSON" | jq 'length' 2>/dev/null || echo '0')"

NOTES="Found $SIDEFX_COUNT side effects and $EDGES_COUNT import edges."

# --- Final output ---
jq -n \
  --argjson pipelines "$PIPELINES_JSON" \
  --argjson edges "$EDGES_JSON" \
  --argjson sidefx "$SIDEFX_JSON" \
  --arg notes "$NOTES" \
  '{
    "pipelines": $pipelines,
    "edges": $edges,
    "sidefx": $sidefx,
    "notes": $notes
  }'
