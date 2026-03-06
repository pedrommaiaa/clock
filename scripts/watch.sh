#!/bin/bash
set -euo pipefail

# watch.sh — Event watcher (polling-based, single run).
# Checks for git pushes and CI failures, emits JSONL events.
# Input JSON: { "repo": ".", "events": ["git", "ci"], "interval": 60 }
# The interval field is informational only — dock handles the loop.

# Check required commands
for cmd in jq; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

# Read input from stdin (or use defaults)
INPUT='{"repo": ".", "events": ["git"]}'
if [ ! -t 0 ]; then
  RAW="$(cat)"
  if [ -n "$RAW" ]; then
    INPUT="$RAW"
  fi
fi

REPO="$(echo "$INPUT" | jq -r '.repo // "."')"
EVENTS="$(echo "$INPUT" | jq -r '.events // ["git"] | .[]')"

# Change to repo directory for git operations
if [ -d "$REPO" ]; then
  cd "$REPO"
fi

# Process each event type
for event_type in $EVENTS; do
  case "$event_type" in

    git)
      # Check for new commits on origin/main
      if command -v git >/dev/null 2>&1 && [ -d ".git" ]; then
        # Fetch latest from remote (suppress output)
        git fetch --quiet 2>/dev/null || true

        # Determine the default branch
        DEFAULT_BRANCH="main"
        if git rev-parse --verify origin/master >/dev/null 2>&1; then
          if ! git rev-parse --verify origin/main >/dev/null 2>&1; then
            DEFAULT_BRANCH="master"
          fi
        fi

        # Check for new commits
        NEW_COMMITS="$(git log "HEAD..origin/${DEFAULT_BRANCH}" --oneline 2>/dev/null || true)"
        if [ -n "$NEW_COMMITS" ]; then
          COMMIT_COUNT="$(echo "$NEW_COMMITS" | wc -l | tr -d ' ')"
          jq -c -n \
            --arg type "git.push" \
            --arg repo "$REPO" \
            --arg branch "$DEFAULT_BRANCH" \
            --argjson commits "$COMMIT_COUNT" \
            '{type: $type, repo: $repo, branch: $branch, meta: {commits: $commits}}'
        fi
      fi
      ;;

    ci)
      # Check for CI failures via gh CLI
      if command -v gh >/dev/null 2>&1; then
        # List recent failed workflow runs
        FAILURES="$(gh run list --status failure --limit 5 --json databaseId,conclusion,name,headBranch 2>/dev/null || echo '[]')"
        FAIL_COUNT="$(echo "$FAILURES" | jq 'length' 2>/dev/null || echo '0')"

        if [ "$FAIL_COUNT" -gt 0 ]; then
          for i in $(seq 0 $((FAIL_COUNT - 1))); do
            RUN_ID="$(echo "$FAILURES" | jq -r ".[$i].databaseId")"
            RUN_NAME="$(echo "$FAILURES" | jq -r ".[$i].name")"
            BRANCH="$(echo "$FAILURES" | jq -r ".[$i].headBranch")"

            jq -c -n \
              --arg type "ci.fail" \
              --arg repo "$REPO" \
              --arg branch "$BRANCH" \
              --arg run_id "$RUN_ID" \
              --arg run_name "$RUN_NAME" \
              '{type: $type, repo: $repo, branch: $branch, meta: {run_id: $run_id, run_name: $run_name}}'
          done
        fi
      else
        # No gh CLI — check for workflow files as a hint
        if [ -d ".github/workflows" ]; then
          # Can't check CI status without gh CLI; emit a note
          : # Silent — no events to emit
        fi
      fi
      ;;

    *)
      # Unknown event type — skip silently
      ;;
  esac
done
