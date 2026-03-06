#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq rg; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

# Read JSON input from stdin
INPUT="{}"
if [ ! -t 0 ]; then
  INPUT="$(cat)"
  if [ -z "$INPUT" ]; then
    INPUT="{}"
  fi
fi

QUERY="$(echo "$INPUT" | jq -r '.q // ""')"
MAX="$(echo "$INPUT" | jq -r '.max // 200')"

if [ -z "$QUERY" ]; then
  echo '{"error":"missing required field: q"}' >&2
  exit 1
fi

# Build rg arguments (flags only)
RG_ARGS=("--json" "--no-heading" "--line-number" "--column")

# Collect search paths separately
SEARCH_PATHS=()
while IFS= read -r p; do
  [ -z "$p" ] && continue
  SEARCH_PATHS+=("$p")
done < <(echo "$INPUT" | jq -r '.paths // [] | .[]' 2>/dev/null)

# Add glob filters (these are flags, go before pattern)
while IFS= read -r g; do
  [ -z "$g" ] && continue
  RG_ARGS+=("--glob" "$g")
done < <(echo "$INPUT" | jq -r '.glob // [] | .[]' 2>/dev/null)

# If no paths specified, search current directory
if [ "${#SEARCH_PATHS[@]}" -eq 0 ]; then
  SEARCH_PATHS+=(".")
fi

# Run ripgrep: rg [FLAGS] PATTERN [PATH...]
COUNTER=0
rg "${RG_ARGS[@]}" -- "$QUERY" "${SEARCH_PATHS[@]}" 2>/dev/null | while IFS= read -r json_line; do
  [ "$COUNTER" -ge "$MAX" ] && break

  TYPE="$(echo "$json_line" | jq -r '.type // ""')"
  [ "$TYPE" != "match" ] && continue

  PATH_VAL="$(echo "$json_line" | jq -r '.data.path.text // ""')"
  LINE_NUM="$(echo "$json_line" | jq -r '.data.line_number // 0')"
  COL_NUM="$(echo "$json_line" | jq -r '.data.submatches[0].start // 0')"
  TEXT="$(echo "$json_line" | jq -r '.data.lines.text // ""' | tr -d '\n')"

  # Score: 1.0 / (position_in_results + 1)
  SCORE="$(echo "scale=4; 1.0 / ($COUNTER + 1)" | bc)"

  jq -cn \
    --arg path "$PATH_VAL" \
    --argjson line "$LINE_NUM" \
    --argjson col "$((COL_NUM + 1))" \
    --arg text "$TEXT" \
    --argjson score "$SCORE" \
    '{"path": $path, "line": $line, "col": $col, "text": $text, "score": $score}'

  COUNTER=$((COUNTER + 1))
done
