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

GOAL="$(echo "$INPUT" | jq -r '.goal // "No goal specified"')"

# --- diffsum ---
DIFFSUM="$(echo "$INPUT" | jq '.diffsum // {}' 2>/dev/null || echo '{}')"
DIFF_FILES="$(echo "$DIFFSUM" | jq -r '.files // []')"
DIFF_ADD="$(echo "$DIFFSUM" | jq -r '.add // 0')"
DIFF_DEL="$(echo "$DIFFSUM" | jq -r '.del // 0')"
FILE_COUNT="$(echo "$DIFF_FILES" | jq 'length' 2>/dev/null || echo '0')"

# --- vrfy ---
VRFY="$(echo "$INPUT" | jq '.vrfy // {}' 2>/dev/null || echo '{}')"
VRFY_OK="$(echo "$VRFY" | jq -r '.ok // "unknown"')"
VRFY_STEPS="$(echo "$VRFY" | jq '.steps // []' 2>/dev/null || echo '[]')"
VRFY_STEPS_COUNT="$(echo "$VRFY_STEPS" | jq 'length' 2>/dev/null || echo '0')"

# --- cites ---
CITES="$(echo "$INPUT" | jq '.cites // []' 2>/dev/null || echo '[]')"
CITES_COUNT="$(echo "$CITES" | jq 'length' 2>/dev/null || echo '0')"

# --- Build Markdown report ---
{
  echo "# Clock Report"
  echo ""
  echo "_Generated: $(date -u '+%Y-%m-%d %H:%M UTC')_"
  echo ""

  # --- Goal ---
  echo "## Goal"
  echo ""
  echo "$GOAL"
  echo ""

  # --- Changes Made ---
  echo "## Changes Made"
  echo ""
  echo "- **Files changed:** $FILE_COUNT"
  echo "- **Lines added:** $DIFF_ADD"
  echo "- **Lines deleted:** $DIFF_DEL"
  echo ""

  if [ "$FILE_COUNT" -gt 0 ]; then
    echo "### Modified Files"
    echo ""
    echo "$DIFF_FILES" | jq -r '.[] | "- `\(.)`"' 2>/dev/null || true
    echo ""
  fi

  # --- Rationale ---
  echo "## Rationale"
  echo ""
  RATIONALE="$(echo "$INPUT" | jq -r '.rationale // ""')"
  if [ -n "$RATIONALE" ]; then
    echo "$RATIONALE"
  else
    echo "Changes were made to achieve the stated goal."
  fi
  echo ""

  # --- Verification ---
  echo "## Verification"
  echo ""

  if [ "$VRFY_OK" = "true" ]; then
    echo "**Overall: PASS**"
  elif [ "$VRFY_OK" = "false" ]; then
    echo "**Overall: FAIL**"
  else
    echo "**Overall: Not verified**"
  fi
  echo ""

  if [ "$VRFY_STEPS_COUNT" -gt 0 ]; then
    echo "| Step | Command | Status | Output |"
    echo "|------|---------|--------|--------|"
    echo "$VRFY_STEPS" | jq -r '.[] |
      "| \(.name // .cmd // "step") | `\(.cmd // "n/a")` | \(if .ok then "PASS" else "FAIL" end) | \(.output // .error // "-" | .[0:60] | gsub("\n"; " ")) |"
    ' 2>/dev/null || true
    echo ""
  fi

  # --- Citations ---
  if [ "$CITES_COUNT" -gt 0 ]; then
    echo "## References"
    echo ""
    echo "$CITES" | jq -r '.[] |
      if .start and .end then
        "- `\(.path)` lines \(.start)-\(.end)"
      elif .path then
        "- `\(.path)`"
      else
        "- \(.)"
      end
    ' 2>/dev/null || true
    echo ""
  fi

  # --- Next Steps ---
  echo "## Next Steps"
  echo ""
  NEXT="$(echo "$INPUT" | jq -r '.next // [] | .[]' 2>/dev/null)"
  if [ -n "$NEXT" ]; then
    while IFS= read -r step; do
      echo "- $step"
    done <<< "$NEXT"
  else
    # Auto-suggest based on verification
    if [ "$VRFY_OK" = "false" ]; then
      echo "- Fix failing verification steps"
      echo "- Re-run verification after fixes"
    elif [ "$VRFY_OK" = "true" ]; then
      echo "- Review changes for edge cases"
      echo "- Consider adding tests for the changes"
      echo "- Commit and push when ready"
    else
      echo "- Run verification commands to validate changes"
      echo "- Review the modified files for correctness"
    fi
  fi
  echo ""
}
