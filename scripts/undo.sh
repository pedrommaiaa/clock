#!/usr/bin/env bash
# undo.sh — Revert to a clock checkpoint.
# Input JSON: { "chk": "checkpoint-id" }
# Output JSON: { "ok": true/false, "did": "restore|revert", "err": "..." }
set -euo pipefail

# Read JSON input from stdin
INPUT=$(cat)
CHK=$(echo "$INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('chk',''))" 2>/dev/null || echo "")

if [ -z "$CHK" ]; then
    echo '{"ok": false, "did": "", "err": "chk is required"}'
    exit 0
fi

LABEL="clock-chk-${CHK}"

# Helper to output JSON
output_json() {
    local ok="$1" did="$2" err="$3"
    printf '{"ok": %s, "did": "%s", "err": "%s"}\n' "$ok" "$did" "$err"
}

# Try to find and pop the stash matching the label
STASH_REF=""
while IFS= read -r line; do
    if echo "$line" | grep -q "$LABEL"; then
        STASH_REF=$(echo "$line" | cut -d: -f1)
        break
    fi
done < <(git stash list 2>/dev/null || true)

if [ -n "$STASH_REF" ]; then
    # Found matching stash — pop it
    if git stash pop "$STASH_REF" 2>/dev/null; then
        output_json "true" "restore" ""
        exit 0
    else
        ERR=$(git stash pop "$STASH_REF" 2>&1 || true)
        output_json "false" "restore" "stash pop failed: ${ERR//\"/\\\"}"
        exit 0
    fi
fi

# No stash found — try to find a commit with the label
COMMIT_SHA=""
COMMIT_SHA=$(git log --all --oneline --grep="$LABEL" --format="%H" -1 2>/dev/null || echo "")

if [ -n "$COMMIT_SHA" ]; then
    # Found matching commit — try to revert
    if git revert --no-edit "$COMMIT_SHA" 2>/dev/null; then
        output_json "true" "revert" ""
        exit 0
    else
        # Revert failed, try reset
        if git reset --hard "${COMMIT_SHA}^" 2>/dev/null; then
            output_json "true" "revert" ""
            exit 0
        else
            ERR="could not revert or reset to commit $COMMIT_SHA"
            output_json "false" "revert" "$ERR"
            exit 0
        fi
    fi
fi

# Nothing found
output_json "false" "" "no checkpoint found for ${CHK}"
exit 0
