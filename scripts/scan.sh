#!/bin/bash
set -euo pipefail

# Check required commands
for cmd in jq find; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "{\"error\":\"missing command: $cmd\"}" >&2; exit 1; }
done

# Read optional JSON config from stdin (or use defaults)
INPUT="{}"
if [ ! -t 0 ]; then
  INPUT="$(cat)"
  if [ -z "$INPUT" ]; then
    INPUT="{}"
  fi
fi

ROOT="$(echo "$INPUT" | jq -r '.root // "."')"
DEEP="$(echo "$INPUT" | jq -r '.deep // false')"

# Resolve to absolute path
ROOT="$(cd "$ROOT" && pwd)"

# --- Detect package managers / build files ---
MANAGERS=()
KEY_FILES=()

declare -A MANAGER_MAP=(
  ["package.json"]="npm"
  ["go.mod"]="go"
  ["pyproject.toml"]="pip"
  ["Cargo.toml"]="cargo"
  ["Makefile"]="make"
  ["Gemfile"]="bundler"
  ["pom.xml"]="maven"
  ["build.gradle"]="gradle"
)

for file in "${!MANAGER_MAP[@]}"; do
  if [ "$DEEP" = "true" ]; then
    found="$(find "$ROOT" -name "$file" -not -path '*/vendor/*' -not -path '*/node_modules/*' 2>/dev/null | head -1)"
  else
    found=""
    [ -f "$ROOT/$file" ] && found="$ROOT/$file"
  fi
  if [ -n "$found" ]; then
    MANAGERS+=("${MANAGER_MAP[$file]}")
    KEY_FILES+=("$file")
  fi
done

# --- Detect CI configs ---
[ -d "$ROOT/.github/workflows" ] && KEY_FILES+=(".github/workflows/")
[ -f "$ROOT/.gitlab-ci.yml" ] && KEY_FILES+=(".gitlab-ci.yml")
[ -f "$ROOT/Jenkinsfile" ] && KEY_FILES+=("Jenkinsfile")

# --- Detect languages from file extensions ---
declare -A LANG_MAP=(
  ["go"]="go"
  ["py"]="python"
  ["js"]="javascript"
  ["ts"]="typescript"
  ["rs"]="rust"
  ["java"]="java"
  ["rb"]="ruby"
  ["c"]="c"
  ["cpp"]="cpp"
  ["h"]="c"
  ["hpp"]="cpp"
  ["sh"]="shell"
  ["lua"]="lua"
  ["zig"]="zig"
  ["swift"]="swift"
  ["kt"]="kotlin"
)

declare -A LANGS_SEEN=()
while IFS= read -r ext; do
  [ -z "$ext" ] && continue
  lang="${LANG_MAP[$ext]:-}"
  if [ -n "$lang" ] && [ -z "${LANGS_SEEN[$lang]:-}" ]; then
    LANGS_SEEN["$lang"]=1
  fi
done < <(find "$ROOT" -type f \
  -not -path '*/vendor/*' \
  -not -path '*/node_modules/*' \
  -not -path '*/.git/*' \
  2>/dev/null | sed 's/.*\.//' | sort -u)

LANGUAGES=()
for lang in "${!LANGS_SEEN[@]}"; do
  LANGUAGES+=("$lang")
done

# --- Infer candidate commands ---
declare -A COMMANDS=()

# From go.mod
if [ -f "$ROOT/go.mod" ]; then
  COMMANDS["test"]="go test ./..."
  COMMANDS["build"]="go build ./..."
  COMMANDS["lint"]="golangci-lint run"
  COMMANDS["format"]="gofmt -w ."
fi

# From package.json scripts
if [ -f "$ROOT/package.json" ]; then
  for script_name in test build lint format start dev; do
    val="$(jq -r ".scripts.\"$script_name\" // empty" "$ROOT/package.json" 2>/dev/null)"
    if [ -n "$val" ]; then
      COMMANDS["$script_name"]="npm run $script_name"
    fi
  done
fi

# From Makefile targets
if [ -f "$ROOT/Makefile" ]; then
  while IFS= read -r target; do
    case "$target" in
      test|lint|build|format|fmt|check|run)
        key="$target"
        [ "$target" = "fmt" ] && key="format"
        COMMANDS["$key"]="make $target"
        ;;
    esac
  done < <(grep -oE '^[a-zA-Z_-]+:' "$ROOT/Makefile" 2>/dev/null | tr -d ':')
fi

# From Cargo.toml
if [ -f "$ROOT/Cargo.toml" ]; then
  COMMANDS["test"]="cargo test"
  COMMANDS["build"]="cargo build"
  COMMANDS["lint"]="cargo clippy"
  COMMANDS["format"]="cargo fmt"
fi

# From pyproject.toml
if [ -f "$ROOT/pyproject.toml" ]; then
  COMMANDS["test"]="pytest"
  COMMANDS["lint"]="ruff check ."
  COMMANDS["format"]="ruff format ."
fi

# --- Build JSON output ---
LANGS_JSON="$(printf '%s\n' "${LANGUAGES[@]}" | jq -R . | jq -s .)"
MANAGERS_JSON="$(printf '%s\n' "${MANAGERS[@]}" | jq -R . | jq -s .)"
KEY_FILES_JSON="$(printf '%s\n' "${KEY_FILES[@]}" | jq -R . | jq -s .)"

COMMANDS_JSON="{}"
for key in "${!COMMANDS[@]}"; do
  COMMANDS_JSON="$(echo "$COMMANDS_JSON" | jq --arg k "$key" --arg v "${COMMANDS[$key]}" '. + {($k): $v}')"
done

jq -n \
  --arg root "$ROOT" \
  --argjson languages "$LANGS_JSON" \
  --argjson managers "$MANAGERS_JSON" \
  --argjson commands "$COMMANDS_JSON" \
  --argjson key_files "$KEY_FILES_JSON" \
  '{
    root: $root,
    languages: $languages,
    managers: $managers,
    commands: $commands,
    key_files: $key_files
  }'
