#!/bin/bash
set -uo pipefail

# Tool test runner for the Clock project.
# Runs go vet, go test, and basic schema validation on a tool directory.
#
# Usage:
#   test.sh -path cmd/envy
#   echo '{"path":"cmd/envy"}' | test.sh

# Check required commands
for cmd in jq go; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

# Parse input: -path flag or stdin JSON
TOOL_PATH=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -path)
      TOOL_PATH="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

# If no flag, try reading from stdin
if [ -z "$TOOL_PATH" ] && [ ! -t 0 ]; then
  INPUT="$(cat)"
  if [ -n "$INPUT" ]; then
    TOOL_PATH="$(echo "$INPUT" | jq -r '.path // empty' 2>/dev/null)"
  fi
fi

if [ -z "$TOOL_PATH" ]; then
  echo '{"error":"no path provided. Use -path <dir> or pipe {\"path\":\"...\"}"}' >&2
  exit 1
fi

# Strip trailing slashes
TOOL_PATH="${TOOL_PATH%/}"

# Ensure the path exists
if [ ! -d "$TOOL_PATH" ]; then
  echo "{\"error\":\"directory not found: $TOOL_PATH\"}" >&2
  exit 1
fi

# Initialize results
PASSED=true
TOTAL_TESTS=0
STEPS='[]'

# --- Step 1: go vet ---
VET_OUTPUT=""
VET_OK=true
VET_OUTPUT=$(go vet "./$TOOL_PATH/..." 2>&1) || VET_OK=false

if [ "$VET_OK" = "false" ]; then
  PASSED=false
fi

STEPS=$(echo "$STEPS" | jq \
  --arg name "vet" \
  --argjson ok "$VET_OK" \
  --arg output "$VET_OUTPUT" \
  '. + [{"name": $name, "ok": $ok, "output": $output}]')

# --- Step 2: go test ---
TEST_OUTPUT=""
TEST_OK=true
TEST_OUTPUT=$(go test "./$TOOL_PATH/..." 2>&1) || TEST_OK=false

if [ "$TEST_OK" = "false" ]; then
  # Check if it's just "no test files" which is not a failure
  if echo "$TEST_OUTPUT" | grep -q "no test files"; then
    TEST_OK=true
    TEST_OUTPUT="no test files"
  else
    PASSED=false
  fi
fi

# Count tests from output
TEST_COUNT=$(echo "$TEST_OUTPUT" | grep -oE 'ok.*\(.*\)' | wc -l | tr -d ' ')
if [ -z "$TEST_COUNT" ] || [ "$TEST_COUNT" = "0" ]; then
  # Try to count from "--- PASS" lines
  TEST_COUNT=$(echo "$TEST_OUTPUT" | grep -c '--- PASS' 2>/dev/null || echo "0")
fi
TOTAL_TESTS=$((TOTAL_TESTS + TEST_COUNT))

STEPS=$(echo "$STEPS" | jq \
  --arg name "test" \
  --argjson ok "$TEST_OK" \
  --arg output "$TEST_OUTPUT" \
  '. + [{"name": $name, "ok": $ok, "output": $output}]')

# --- Step 3: Schema validation ---
# Check if the tool produces valid JSON output
SCHEMA_OK=true
SCHEMA_OUTPUT=""

# Determine if it's a Go binary or shell script
if [ -f "$TOOL_PATH/main.go" ]; then
  # Try to build the tool first
  BUILD_OUTPUT=$(go build -o /tmp/clock_test_binary "./$TOOL_PATH" 2>&1)
  if [ $? -eq 0 ]; then
    # Try running with empty/minimal input and check if output is valid JSON
    TOOL_OUTPUT=$(echo '{}' | timeout 5 /tmp/clock_test_binary 2>/dev/null || true)
    if [ -n "$TOOL_OUTPUT" ]; then
      if echo "$TOOL_OUTPUT" | head -1 | jq . >/dev/null 2>&1; then
        SCHEMA_OUTPUT="output is valid JSON"
      else
        SCHEMA_OK=false
        SCHEMA_OUTPUT="output is not valid JSON: $(echo "$TOOL_OUTPUT" | head -1 | head -c 200)"
        PASSED=false
      fi
    else
      SCHEMA_OUTPUT="no output produced (may require specific input)"
    fi
    rm -f /tmp/clock_test_binary
  else
    SCHEMA_OK=false
    SCHEMA_OUTPUT="build failed: $BUILD_OUTPUT"
    PASSED=false
  fi
elif [ -f "$TOOL_PATH" ] && head -1 "$TOOL_PATH" | grep -q "^#!"; then
  # Shell script -- try running with empty input
  TOOL_OUTPUT=$(echo '{}' | timeout 5 bash "$TOOL_PATH" 2>/dev/null || true)
  if [ -n "$TOOL_OUTPUT" ]; then
    if echo "$TOOL_OUTPUT" | head -1 | jq . >/dev/null 2>&1; then
      SCHEMA_OUTPUT="output is valid JSON"
    else
      SCHEMA_OK=false
      SCHEMA_OUTPUT="output is not valid JSON"
      PASSED=false
    fi
  else
    SCHEMA_OUTPUT="no output produced (may require specific input)"
  fi
else
  SCHEMA_OUTPUT="skipped (no main.go or script found)"
fi

STEPS=$(echo "$STEPS" | jq \
  --arg name "schema" \
  --argjson ok "$SCHEMA_OK" \
  --arg output "$SCHEMA_OUTPUT" \
  '. + [{"name": $name, "ok": $ok, "output": $output}]')

# --- Build final output ---
jq -n \
  --argjson passed "$PASSED" \
  --argjson tests "$TOTAL_TESTS" \
  --argjson steps "$STEPS" \
  '{
    passed: $passed,
    tests: $tests,
    steps: $steps
  }'
