#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq rg; do
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

MAX="$(echo "$INPUT" | jq -r '.max // 2000')"

# Collect paths to scan
PATHS=()
while IFS= read -r p; do
  [ -z "$p" ] && continue
  PATHS+=("$p")
done < <(echo "$INPUT" | jq -r '.paths // ["."] | .[]' 2>/dev/null)

if [ "${#PATHS[@]}" -eq 0 ]; then
  PATHS=(".")
fi

# Optional language filters for rg --glob
LANG_GLOBS=()
while IFS= read -r lang; do
  [ -z "$lang" ] && continue
  case "$lang" in
    go)         LANG_GLOBS+=("--glob" "*.go") ;;
    python|py)  LANG_GLOBS+=("--glob" "*.py") ;;
    javascript|js) LANG_GLOBS+=("--glob" "*.js") ;;
    typescript|ts) LANG_GLOBS+=("--glob" "*.ts" "--glob" "*.tsx") ;;
    rust|rs)    LANG_GLOBS+=("--glob" "*.rs") ;;
    java)       LANG_GLOBS+=("--glob" "*.java") ;;
    ruby|rb)    LANG_GLOBS+=("--glob" "*.rb") ;;
    *)          ;;
  esac
done < <(echo "$INPUT" | jq -r '.langs // [] | .[]' 2>/dev/null)

# Common rg args
RG_BASE=(rg --no-heading --line-number --no-filename -n)
if [ "${#LANG_GLOBS[@]}" -gt 0 ]; then
  RG_BASE+=("${LANG_GLOBS[@]}")
fi

# Helper: run rg for a pattern across all paths, output JSONL entries
# Args: category pattern [kind_override]
rg_extract() {
  local category="$1"
  local pattern="$2"
  local kind="${3:-$category}"

  for scan_path in "${PATHS[@]}"; do
    rg --no-heading --line-number --with-filename \
      ${LANG_GLOBS[@]+"${LANG_GLOBS[@]}"} \
      -e "$pattern" "$scan_path" 2>/dev/null || true
  done | head -n "$MAX" | while IFS= read -r line; do
    [ -z "$line" ] && continue
    # Parse rg output: path:line:text
    local fpath linenum text name
    fpath="$(echo "$line" | cut -d':' -f1)"
    linenum="$(echo "$line" | cut -d':' -f2)"
    text="$(echo "$line" | cut -d':' -f3-)"

    # Extract a name from the match
    name="$(echo "$text" | sed -E 's/^[[:space:]]*//' | head -c 120)"

    jq -cn \
      --arg name "$name" \
      --arg path "$fpath" \
      --argjson line "${linenum:-0}" \
      --arg kind "$kind" \
      --arg category "$category" \
      '{"name": $name, "path": $path, "line": $line, "kind": $kind, "category": $category}'
  done
}

# Temporary file for all matches
MATCHES="$(mktemp)"
trap 'rm -f "$MATCHES"' EXIT

# --- Exports ---
rg_extract "exports" 'export\s+(function|class|const|default|let|var)\s' "export" >> "$MATCHES"
rg_extract "exports" 'module\.exports' "module.exports" >> "$MATCHES"
rg_extract "exports" '^func\s+[A-Z]' "go-export" >> "$MATCHES"
rg_extract "exports" 'pub\s+(fn|struct|enum|trait|mod)\s' "rust-export" >> "$MATCHES"

# --- Routes ---
rg_extract "routes" 'app\.(get|post|put|delete|patch|use)\(' "http-route" >> "$MATCHES"
rg_extract "routes" 'router\.' "router" >> "$MATCHES"
rg_extract "routes" '@(Get|Post|Put|Delete|Patch|RequestMapping)\(' "decorator-route" >> "$MATCHES"
rg_extract "routes" 'http\.(Handle|HandleFunc)\(' "go-route" >> "$MATCHES"

# --- CLI flags ---
rg_extract "cli" 'flag\.' "go-flag" >> "$MATCHES"
rg_extract "cli" 'argparse' "argparse" >> "$MATCHES"
rg_extract "cli" 'commander' "commander" >> "$MATCHES"
rg_extract "cli" 'yargs' "yargs" >> "$MATCHES"
rg_extract "cli" '\.add_argument\(' "cli-arg" >> "$MATCHES"
rg_extract "cli" 'cobra\.' "cobra" >> "$MATCHES"

# --- Env vars ---
rg_extract "env" 'process\.env\.' "node-env" >> "$MATCHES"
rg_extract "env" 'os\.Getenv\(' "go-env" >> "$MATCHES"
rg_extract "env" 'os\.environ' "python-env" >> "$MATCHES"
rg_extract "env" 'env::' "rust-env" >> "$MATCHES"
rg_extract "env" 'viper\.' "viper" >> "$MATCHES"

# --- Schemas ---
rg_extract "schemas" 'type\s+\w+\s+struct\s*\{' "go-struct" >> "$MATCHES"
rg_extract "schemas" 'interface\s+\w+' "interface" >> "$MATCHES"
rg_extract "schemas" 'type\s+\w+\s*=' "type-alias" >> "$MATCHES"
rg_extract "schemas" 'schema' "schema" >> "$MATCHES"

# --- Aggregate into categorized JSON ---
TOTAL="$(wc -l < "$MATCHES" | tr -d ' ')"

# Cap total entries
if [ "$TOTAL" -gt "$MAX" ]; then
  head -n "$MAX" "$MATCHES" > "${MATCHES}.tmp"
  mv "${MATCHES}.tmp" "$MATCHES"
fi

EXPORTS="$(jq -s '[.[] | select(.category == "exports")] | unique_by(.path + ":" + (.line | tostring))' "$MATCHES" 2>/dev/null || echo '[]')"
ROUTES="$(jq -s '[.[] | select(.category == "routes")] | unique_by(.path + ":" + (.line | tostring))' "$MATCHES" 2>/dev/null || echo '[]')"
CLI="$(jq -s '[.[] | select(.category == "cli")] | unique_by(.path + ":" + (.line | tostring))' "$MATCHES" 2>/dev/null || echo '[]')"
ENV="$(jq -s '[.[] | select(.category == "env")] | unique_by(.path + ":" + (.line | tostring))' "$MATCHES" 2>/dev/null || echo '[]')"
SCHEMAS="$(jq -s '[.[] | select(.category == "schemas")] | unique_by(.path + ":" + (.line | tostring))' "$MATCHES" 2>/dev/null || echo '[]')"

jq -n \
  --argjson exports "$EXPORTS" \
  --argjson routes "$ROUTES" \
  --argjson cli "$CLI" \
  --argjson env "$ENV" \
  --argjson schemas "$SCHEMAS" \
  '{
    "exports": $exports,
    "routes": $routes,
    "cli": $cli,
    "env": $env,
    "schemas": $schemas
  }'
