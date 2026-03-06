#!/bin/bash
set -euo pipefail

# tick.sh — Scheduler that reads .clock/schedules.json and enqueues due tasks.
# Supports daily ("0 H * * *") and hourly ("M * * * *") cron patterns.
# Outputs JSONL events and pipes them to `q put` if available.

# Check required commands
for cmd in jq date; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

SCHEDULES_FILE=".clock/schedules.json"
STATE_FILE=".clock/tick_state.json"

# Load schedules (or empty)
if [ ! -f "$SCHEDULES_FILE" ]; then
  # No schedules configured — nothing to do
  exit 0
fi

TASKS="$(jq -r '.tasks // []' "$SCHEDULES_FILE")"
TASK_COUNT="$(echo "$TASKS" | jq 'length')"

if [ "$TASK_COUNT" -eq 0 ]; then
  exit 0
fi

# Load tick state (last run times)
if [ -f "$STATE_FILE" ]; then
  STATE="$(cat "$STATE_FILE")"
else
  STATE="{}"
fi

NOW_EPOCH="$(date +%s)"
NOW_HOUR="$(date -u +%H)"
NOW_MIN="$(date -u +%M)"

# Helper: check if a task is due based on its cron pattern
# Supports:
#   "0 H * * *"  — daily at hour H  (matches if current hour == H and not run today)
#   "M * * * *"  — hourly at minute M (matches if current min == M and not run this hour)
is_due() {
  local cron="$1"
  local task_name="$2"

  # Parse cron fields
  local cron_min cron_hour
  cron_min="$(echo "$cron" | awk '{print $1}')"
  cron_hour="$(echo "$cron" | awk '{print $2}')"

  # Get last run timestamp for this task
  local last_run
  last_run="$(echo "$STATE" | jq -r --arg name "$task_name" '.[$name] // 0')"

  if [ "$cron_hour" != "*" ] && [ "$cron_min" != "*" ]; then
    # Daily pattern: "M H * * *" — run once per day at H:M
    if [ "$NOW_HOUR" = "$(printf '%02d' "$cron_hour")" ]; then
      # Check if already run today
      local today_start
      today_start="$(date -u -j -f '%Y-%m-%d %H:%M:%S' "$(date -u '+%Y-%m-%d') 00:00:00" +%s 2>/dev/null || date -u -d "$(date -u '+%Y-%m-%d') 00:00:00" +%s 2>/dev/null || echo 0)"
      if [ "$last_run" -lt "$today_start" ]; then
        return 0
      fi
    fi
  elif [ "$cron_hour" = "*" ] && [ "$cron_min" != "*" ]; then
    # Hourly pattern: "M * * * *" — run once per hour at minute M
    if [ "$((10#$NOW_MIN))" -ge "$((10#$cron_min))" ]; then
      # Check if already run this hour
      local hour_start
      hour_start="$(( NOW_EPOCH - (10#$NOW_MIN * 60) - $(date -u +%S) ))"
      if [ "$last_run" -lt "$hour_start" ]; then
        return 0
      fi
    fi
  elif [ "$cron_hour" != "*" ] && [ "$cron_min" = "*" ]; then
    # Run every minute during hour H — treat as daily at that hour
    if [ "$NOW_HOUR" = "$(printf '%02d' "$cron_hour")" ]; then
      local today_start
      today_start="$(date -u -j -f '%Y-%m-%d %H:%M:%S' "$(date -u '+%Y-%m-%d') 00:00:00" +%s 2>/dev/null || date -u -d "$(date -u '+%Y-%m-%d') 00:00:00" +%s 2>/dev/null || echo 0)"
      if [ "$last_run" -lt "$today_start" ]; then
        return 0
      fi
    fi
  fi

  return 1
}

# Check if q command is available for enqueuing
HAS_Q=false
command -v q >/dev/null 2>&1 && HAS_Q=true

# Iterate tasks
for i in $(seq 0 $((TASK_COUNT - 1))); do
  TASK="$(echo "$TASKS" | jq ".[$i]")"
  TASK_NAME="$(echo "$TASK" | jq -r '.name')"
  TASK_CRON="$(echo "$TASK" | jq -r '.cron // "0 2 * * *"')"
  TASK_GOAL="$(echo "$TASK" | jq -r '.goal // ""')"

  if is_due "$TASK_CRON" "$TASK_NAME"; then
    # Build event
    EVENT="$(jq -n \
      --arg type "schedule" \
      --arg name "$TASK_NAME" \
      --arg goal "$TASK_GOAL" \
      --argjson ts "$NOW_EPOCH" \
      '{type: $type, repo: ".", meta: {task: $name, goal: $goal, ts: $ts}}')"

    # Enqueue if q is available
    ENQUEUED=false
    if [ "$HAS_Q" = true ]; then
      if echo "$EVENT" | q put 2>/dev/null; then
        ENQUEUED=true
      fi
    fi

    # Output JSONL
    jq -c -n \
      --arg type "schedule" \
      --arg task "$TASK_NAME" \
      --argjson enqueued "$ENQUEUED" \
      '{type: $type, task: $task, enqueued: $enqueued}'

    # Update state with current timestamp
    STATE="$(echo "$STATE" | jq --arg name "$TASK_NAME" --argjson ts "$NOW_EPOCH" '. + {($name): $ts}')"
  fi
done

# Save updated state
mkdir -p "$(dirname "$STATE_FILE")"
echo "$STATE" | jq '.' > "$STATE_FILE"
