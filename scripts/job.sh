#!/bin/bash
set -euo pipefail

# job.sh — Event-to-job compiler.
# Reads an event JSON from stdin, matches against rules in .clock/job_rules.json,
# and outputs a JobSpec JSON.

# Check required commands
for cmd in jq; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

RULES_FILE=".clock/job_rules.json"

# Read event from stdin
INPUT="{}"
if [ ! -t 0 ]; then
  INPUT="$(cat)"
  if [ -z "$INPUT" ]; then
    INPUT="{}"
  fi
fi

# Extract event type
EVENT_TYPE="$(echo "$INPUT" | jq -r '.type // ""')"

if [ -z "$EVENT_TYPE" ]; then
  echo '{"error":"no event type in input"}' >&2
  exit 1
fi

# Load rules (or default empty)
if [ -f "$RULES_FILE" ]; then
  RULES="$(cat "$RULES_FILE")"
else
  RULES="[]"
fi

RULE_COUNT="$(echo "$RULES" | jq 'length')"

# Find matching rule
MATCHED=false
for i in $(seq 0 $((RULE_COUNT - 1))); do
  RULE="$(echo "$RULES" | jq ".[$i]")"

  # Check if event type matches the rule's match.type
  MATCH_TYPE="$(echo "$RULE" | jq -r '.match.type // ""')"

  if [ "$MATCH_TYPE" = "$EVENT_TYPE" ]; then
    MATCHED=true

    # Extract rule fields
    GOAL="$(echo "$RULE" | jq -r '.goal // "handle event"')"
    REPO="$(echo "$INPUT" | jq -r '.repo // "."')"
    SCOPE="$(echo "$RULE" | jq '.scope // []')"
    PRIORITY="$(echo "$RULE" | jq -r '.priority // "medium"')"
    PLAN="$(echo "$RULE" | jq '.plan // []')"

    # Generate job ID from timestamp
    JOB_ID="$(date +%s%N | head -c 16)"

    # Output JobSpec
    jq -n \
      --arg id "$JOB_ID" \
      --arg goal "$GOAL" \
      --arg repo "$REPO" \
      --argjson scope "$SCOPE" \
      --arg priority "$PRIORITY" \
      --argjson plan "$PLAN" \
      '{
        id: $id,
        goal: $goal,
        repo: $repo,
        scope: $scope,
        priority: $priority,
        plan: $plan
      }'

    break
  fi
done

# No matching rule found
if [ "$MATCHED" = false ]; then
  jq -n \
    --argjson matched false \
    --arg event_type "$EVENT_TYPE" \
    '{matched: $matched, event_type: $event_type}'
fi
