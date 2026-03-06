#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq; do
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

MAX_BYTES="$(echo "$INPUT" | jq -r '.max_bytes // 40000')"

# Extract sub-objects
MAP_JSON="$(echo "$INPUT" | jq '.map // {}' 2>/dev/null || echo '{}')"
CTRT_JSON="$(echo "$INPUT" | jq '.ctrt // {}' 2>/dev/null || echo '{}')"
FLOW_JSON="$(echo "$INPUT" | jq '.flow // {}' 2>/dev/null || echo '{}')"

# Start building Markdown output
{
  echo "# Project Dossier"
  echo ""
  echo "_Generated: $(date -u '+%Y-%m-%d %H:%M UTC')_"
  echo ""

  # --- Overview ---
  echo "## Overview"
  echo ""

  TOTAL_FILES="$(echo "$MAP_JSON" | jq -r '.total_files // "unknown"')"
  echo "- **Total files scanned:** $TOTAL_FILES"

  # Directory structure summary
  OUTLINE_COUNT="$(echo "$MAP_JSON" | jq '.outline // [] | length' 2>/dev/null || echo '0')"
  if [ "$OUTLINE_COUNT" -gt 0 ]; then
    echo "- **Top directories:**"
    echo "$MAP_JSON" | jq -r '.outline // [] | .[:15] | .[] | "  - `\(.path)` (\(.files) files)"' 2>/dev/null || true
  fi

  # Key files
  KEY_FILES_COUNT="$(echo "$MAP_JSON" | jq '.key_files // [] | length' 2>/dev/null || echo '0')"
  if [ "$KEY_FILES_COUNT" -gt 0 ]; then
    echo ""
    echo "- **Key files:**"
    echo "$MAP_JSON" | jq -r '.key_files // [] | .[] | "  - `\(.)`"' 2>/dev/null || true
  fi
  echo ""

  # --- Module Roles ---
  echo "## Module Roles"
  echo ""
  if [ "$OUTLINE_COUNT" -gt 0 ]; then
    echo "$MAP_JSON" | jq -r '
      .outline // [] | .[:20] | .[] |
      "- **`\(.path)`** - \(.files) files"
    ' 2>/dev/null || echo "- No module data available."
  else
    echo "- No module data available."
  fi
  echo ""

  # --- Hot Spots ---
  HOT_SPOTS_COUNT="$(echo "$MAP_JSON" | jq '.hot_spots // [] | length' 2>/dev/null || echo '0')"
  if [ "$HOT_SPOTS_COUNT" -gt 0 ]; then
    echo "## Hot Spots"
    echo ""
    echo "$MAP_JSON" | jq -r '.hot_spots // [] | .[] | "- `\(.path)` (\(.reason): \(.value))"' 2>/dev/null || true
    echo ""
  fi

  # --- Contracts & Invariants ---
  echo "## Contracts & Invariants"
  echo ""

  # Exports
  EXPORT_COUNT="$(echo "$CTRT_JSON" | jq '.exports // [] | length' 2>/dev/null || echo '0')"
  if [ "$EXPORT_COUNT" -gt 0 ]; then
    echo "### Exports ($EXPORT_COUNT)"
    echo ""
    echo "$CTRT_JSON" | jq -r '
      .exports // [] | .[:30] | .[] |
      "- `\(.path):\(.line)` \(.kind): \(.name | .[0:80])"
    ' 2>/dev/null || true
    if [ "$EXPORT_COUNT" -gt 30 ]; then
      echo "- _...and $((EXPORT_COUNT - 30)) more_"
    fi
    echo ""
  fi

  # Routes
  ROUTE_COUNT="$(echo "$CTRT_JSON" | jq '.routes // [] | length' 2>/dev/null || echo '0')"
  if [ "$ROUTE_COUNT" -gt 0 ]; then
    echo "### Routes ($ROUTE_COUNT)"
    echo ""
    echo "$CTRT_JSON" | jq -r '
      .routes // [] | .[:20] | .[] |
      "- `\(.path):\(.line)` \(.name | .[0:80])"
    ' 2>/dev/null || true
    echo ""
  fi

  # CLI flags
  CLI_COUNT="$(echo "$CTRT_JSON" | jq '.cli // [] | length' 2>/dev/null || echo '0')"
  if [ "$CLI_COUNT" -gt 0 ]; then
    echo "### CLI Flags ($CLI_COUNT)"
    echo ""
    echo "$CTRT_JSON" | jq -r '
      .cli // [] | .[:20] | .[] |
      "- `\(.path):\(.line)` \(.name | .[0:80])"
    ' 2>/dev/null || true
    echo ""
  fi

  # Env vars
  ENV_COUNT="$(echo "$CTRT_JSON" | jq '.env // [] | length' 2>/dev/null || echo '0')"
  if [ "$ENV_COUNT" -gt 0 ]; then
    echo "### Environment Variables ($ENV_COUNT)"
    echo ""
    echo "$CTRT_JSON" | jq -r '
      .env // [] | .[:20] | .[] |
      "- `\(.path):\(.line)` \(.name | .[0:80])"
    ' 2>/dev/null || true
    echo ""
  fi

  # Schemas
  SCHEMA_COUNT="$(echo "$CTRT_JSON" | jq '.schemas // [] | length' 2>/dev/null || echo '0')"
  if [ "$SCHEMA_COUNT" -gt 0 ]; then
    echo "### Schemas ($SCHEMA_COUNT)"
    echo ""
    echo "$CTRT_JSON" | jq -r '
      .schemas // [] | .[:20] | .[] |
      "- `\(.path):\(.line)` \(.kind): \(.name | .[0:80])"
    ' 2>/dev/null || true
    echo ""
  fi

  # --- Data Flow ---
  echo "## Data Flow"
  echo ""

  PIPELINE_COUNT="$(echo "$FLOW_JSON" | jq '.pipelines // [] | length' 2>/dev/null || echo '0')"
  if [ "$PIPELINE_COUNT" -gt 0 ]; then
    echo "### Pipelines"
    echo ""
    echo "$FLOW_JSON" | jq -r '
      .pipelines // [] | .[:10] | .[] |
      "- **\(.name)**: \(.steps | join(" -> "))"
    ' 2>/dev/null || true
    echo ""
  fi

  SIDEFX_COUNT="$(echo "$FLOW_JSON" | jq '.sidefx // [] | length' 2>/dev/null || echo '0')"
  if [ "$SIDEFX_COUNT" -gt 0 ]; then
    echo "### Side Effects ($SIDEFX_COUNT)"
    echo ""
    echo "$FLOW_JSON" | jq -r '
      .sidefx // [] | .[:30] | .[] |
      "- \(.)"
    ' 2>/dev/null || true
    if [ "$SIDEFX_COUNT" -gt 30 ]; then
      echo "- _...and $((SIDEFX_COUNT - 30)) more_"
    fi
    echo ""
  fi

  EDGE_COUNT="$(echo "$FLOW_JSON" | jq '.edges // [] | length' 2>/dev/null || echo '0')"
  if [ "$EDGE_COUNT" -gt 0 ]; then
    echo "### Dependency Edges ($EDGE_COUNT)"
    echo ""
    echo "$FLOW_JSON" | jq -r '
      .edges // [] | .[:20] | .[] |
      "- `\(.from)` --[\(.kind)]--> `\(.to)`"
    ' 2>/dev/null || true
    if [ "$EDGE_COUNT" -gt 20 ]; then
      echo "- _...and $((EDGE_COUNT - 20)) more_"
    fi
    echo ""
  fi

  FLOW_NOTES="$(echo "$FLOW_JSON" | jq -r '.notes // ""' 2>/dev/null || echo '')"
  if [ -n "$FLOW_NOTES" ]; then
    echo "> $FLOW_NOTES"
    echo ""
  fi

  # --- Safe Zones ---
  echo "## Safe Zones"
  echo ""
  echo "Low-risk areas (tests, docs, examples):"
  echo ""
  echo "$MAP_JSON" | jq -r '
    .outline // [] | .[] |
    select(.path | test("test|spec|__test__|_test|docs|doc|example|fixture|mock|stub"; "i")) |
    "- `\(.path)` (\(.files) files)"
  ' 2>/dev/null || echo "- No test/doc directories detected."
  echo ""

  # --- Risky Zones ---
  echo "## Risky Zones"
  echo ""
  echo "High-impact areas (config, migrations, CI, infra):"
  echo ""
  # From key files
  echo "$MAP_JSON" | jq -r '
    .key_files // [] | .[] |
    select(test("config|migration|ci|deploy|infra|docker|helm|terraform|Makefile|Dockerfile"; "i")) |
    "- `\(.)`"
  ' 2>/dev/null || true
  # From outline
  echo "$MAP_JSON" | jq -r '
    .outline // [] | .[] |
    select(.path | test("config|migration|deploy|infra|ci"; "i")) |
    "- `\(.path)` (\(.files) files)"
  ' 2>/dev/null || true
  echo ""

  # --- Verification Commands ---
  echo "## Verification Commands"
  echo ""
  echo "Check project scan results for detected commands (test, build, lint, format)."
  echo ""

} | head -c "$MAX_BYTES"
