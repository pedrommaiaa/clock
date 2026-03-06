#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

# Read JSON input from stdin into a temp file for safe processing
INPUT_FILE="$(mktemp)"
trap 'rm -f "$INPUT_FILE"' EXIT

if [ ! -t 0 ]; then
  cat > "$INPUT_FILE"
  # If empty, write default
  if [ ! -s "$INPUT_FILE" ]; then
    echo '{}' > "$INPUT_FILE"
  fi
else
  echo '{}' > "$INPUT_FILE"
fi

GOAL="$(jq -r '.goal // "No goal specified"' < "$INPUT_FILE")"
MAX_BYTES="$(jq -r '.max_bytes // 120000' < "$INPUT_FILE")"
DOSS_PATH="$(jq -r '.doss // ""' < "$INPUT_FILE")"

# --- Load dossier content into a temp file ---
DOSS_FILE="$(mktemp)"
trap 'rm -f "$INPUT_FILE" "$DOSS_FILE"' EXIT

if [ -n "$DOSS_PATH" ] && [ -f "$DOSS_PATH" ]; then
  cat "$DOSS_PATH" > "$DOSS_FILE"
elif jq -e '.doss_inline' < "$INPUT_FILE" >/dev/null 2>&1; then
  jq -r '.doss_inline // ""' < "$INPUT_FILE" > "$DOSS_FILE"
fi

# If no dossier, use a minimal system prompt
if [ ! -s "$DOSS_FILE" ]; then
  echo "No project dossier available. Proceed with the information provided in the slices." > "$DOSS_FILE"
fi

# --- Build user content and citations via jq ---
# Use jq to safely assemble the code context from slices (avoids shell escaping issues)
USER_FILE="$(mktemp)"
CITE_FILE="$(mktemp)"
trap 'rm -f "$INPUT_FILE" "$DOSS_FILE" "$USER_FILE" "$CITE_FILE"' EXIT

jq -r '
  .slices // [] | to_entries[] |
  "--- \(.value.path // "unknown") (lines \(.value.start // 1)-\(.value.end // "?")) ---",
  (.value.text // ""),
  ""
' < "$INPUT_FILE" > "$USER_FILE" 2>/dev/null || true

# Build citations JSON
jq '[.slices // [] | .[] | {"path": (.path // "unknown"), "start": (.start // 1), "end": (.end // 0)}]' \
  < "$INPUT_FILE" > "$CITE_FILE" 2>/dev/null || echo '[]' > "$CITE_FILE"

# Prepend goal to user content
USER_HEADER="Goal: ${GOAL}"
if [ -s "$USER_FILE" ]; then
  USER_HEADER="${USER_HEADER}

Relevant code:
"
fi

# --- Enforce max_bytes by truncating ---
SYSTEM_BYTES="$(wc -c < "$DOSS_FILE" | tr -d ' ')"
USER_HEADER_BYTES="${#USER_HEADER}"
USER_SLICE_BYTES="$(wc -c < "$USER_FILE" | tr -d ' ')"
TOTAL_BYTES=$((SYSTEM_BYTES + USER_HEADER_BYTES + USER_SLICE_BYTES))

if [ "$TOTAL_BYTES" -gt "$MAX_BYTES" ]; then
  # Reserve 30% for system, 70% for user content
  SYSTEM_BUDGET=$((MAX_BYTES * 30 / 100))
  USER_BUDGET=$((MAX_BYTES * 70 / 100))

  if [ "$SYSTEM_BYTES" -gt "$SYSTEM_BUDGET" ]; then
    truncate -s "$SYSTEM_BUDGET" "$DOSS_FILE" 2>/dev/null || head -c "$SYSTEM_BUDGET" "$DOSS_FILE" > "${DOSS_FILE}.tmp" && mv "${DOSS_FILE}.tmp" "$DOSS_FILE"
    printf '\n\n[... dossier truncated to fit budget ...]' >> "$DOSS_FILE"
  fi

  SLICE_BUDGET=$((USER_BUDGET - USER_HEADER_BYTES))
  if [ "$SLICE_BUDGET" -lt 0 ]; then SLICE_BUDGET=0; fi
  if [ "$USER_SLICE_BYTES" -gt "$SLICE_BUDGET" ]; then
    head -c "$SLICE_BUDGET" "$USER_FILE" > "${USER_FILE}.tmp" && mv "${USER_FILE}.tmp" "$USER_FILE"
    printf '\n\n[... slices truncated to fit budget ...]' >> "$USER_FILE"
  fi
fi

# --- Build output JSON using jq with file inputs for safe string handling ---
SLICES_TEXT="$(cat "$USER_FILE")"
FULL_USER="${USER_HEADER}${SLICES_TEXT}"

jq -n \
  --rawfile system "$DOSS_FILE" \
  --arg user_content "$FULL_USER" \
  --slurpfile citations "$CITE_FILE" \
  '{
    "system": ($system | rtrimstr("\n")),
    "messages": [
      {
        "role": "user",
        "content": $user_content
      }
    ],
    "tools": [],
    "policy": {},
    "citations": $citations[0]
  }'
