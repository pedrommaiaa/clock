#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq sed wc; do
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

FILE_PATH="$(echo "$INPUT" | jq -r '.path // ""')"
START="$(echo "$INPUT" | jq -r '.start // 1')"
END="$(echo "$INPUT" | jq -r '.end // 0')"
PAD="$(echo "$INPUT" | jq -r '.pad // 20')"

if [ -z "$FILE_PATH" ]; then
  echo '{"error":"missing required field: path"}' >&2
  exit 1
fi

if [ ! -f "$FILE_PATH" ]; then
  echo "{\"error\":\"file not found: $FILE_PATH\"}" >&2
  exit 1
fi

# Get total line count
TOTAL="$(wc -l < "$FILE_PATH" | tr -d ' ')"

# If end is 0 or not set, default to end of file
if [ "$END" -eq 0 ]; then
  END="$TOTAL"
fi

# Apply padding
ACTUAL_START=$((START - PAD))
ACTUAL_END=$((END + PAD))

# Clamp to file bounds
if [ "$ACTUAL_START" -lt 1 ]; then
  ACTUAL_START=1
fi
if [ "$ACTUAL_END" -gt "$TOTAL" ]; then
  ACTUAL_END="$TOTAL"
fi

# Handle edge case: empty file or invalid range
if [ "$TOTAL" -eq 0 ] || [ "$ACTUAL_START" -gt "$TOTAL" ]; then
  jq -n \
    --arg path "$FILE_PATH" \
    --argjson start "$ACTUAL_START" \
    --argjson end "$ACTUAL_END" \
    --arg text "" \
    '{"path": $path, "start": $start, "end": $end, "text": $text}'
  exit 0
fi

# Extract lines using sed
TEXT="$(sed -n "${ACTUAL_START},${ACTUAL_END}p" "$FILE_PATH")"

jq -n \
  --arg path "$FILE_PATH" \
  --argjson start "$ACTUAL_START" \
  --argjson end "$ACTUAL_END" \
  --arg text "$TEXT" \
  '{"path": $path, "start": $start, "end": $end, "text": $text}'
