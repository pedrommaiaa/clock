#!/bin/bash
set -euo pipefail

# Clock installer
# Usage: curl -fsSL https://raw.githubusercontent.com/pedrommaiaa/clock/main/install.sh | bash

REPO="pedrommaiaa/clock"
INSTALL_DIR="${CLOCK_INSTALL_DIR:-/usr/local/bin}"
CLONE_DIR="${CLOCK_CLONE_DIR:-$HOME/.clock-src}"
BRANCH="${CLOCK_BRANCH:-main}"
MIN_GO_VERSION="1.21"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()    { echo -e "${CYAN}>${NC} $*"; }
success() { echo -e "${GREEN}>${NC} $*"; }
warn()    { echo -e "${YELLOW}>${NC} $*"; }
error()   { echo -e "${RED}>${NC} $*" >&2; }
fatal()   { error "$*"; exit 1; }

header() {
  echo ""
  echo -e "${BOLD}Clock Installer${NC}"
  echo -e "Modular AI-assisted engineering system"
  echo ""
}

# --- Dependency checks ---

check_cmd() {
  command -v "$1" >/dev/null 2>&1
}

require_cmd() {
  if ! check_cmd "$1"; then
    fatal "$1 is required but not installed. $2"
  fi
}

check_go_version() {
  local version
  version=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | sed 's/go//')
  local major minor
  major=$(echo "$version" | cut -d. -f1)
  minor=$(echo "$version" | cut -d. -f2)
  local req_major req_minor
  req_major=$(echo "$MIN_GO_VERSION" | cut -d. -f1)
  req_minor=$(echo "$MIN_GO_VERSION" | cut -d. -f2)

  if [ "$major" -lt "$req_major" ] || { [ "$major" -eq "$req_major" ] && [ "$minor" -lt "$req_minor" ]; }; then
    fatal "Go $MIN_GO_VERSION+ required, found $version"
  fi
}

check_dependencies() {
  info "Checking dependencies..."

  require_cmd "go"    "Install from https://go.dev/dl/"
  check_go_version
  success "go $(go version | grep -oE 'go[0-9]+\.[0-9]+\.[0-9]+')"

  require_cmd "git"   "Install from https://git-scm.com/"
  success "git $(git --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"

  if check_cmd "rg"; then
    success "rg $(rg --version | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"
  else
    warn "rg (ripgrep) not found — some tools will not work"
    warn "Install: brew install ripgrep / apt install ripgrep"
  fi

  if check_cmd "jq"; then
    success "jq $(jq --version 2>/dev/null | grep -oE '[0-9]+\.[0-9.]+')"
  else
    warn "jq not found — shell tools will not work"
    warn "Install: brew install jq / apt install jq"
  fi

  echo ""
}

# --- Clone / update ---

fetch_source() {
  if [ -d "$CLONE_DIR/.git" ]; then
    info "Updating existing source in $CLONE_DIR..."
    cd "$CLONE_DIR"
    git fetch origin "$BRANCH" --quiet
    git checkout "$BRANCH" --quiet 2>/dev/null || git checkout -b "$BRANCH" "origin/$BRANCH" --quiet
    git reset --hard "origin/$BRANCH" --quiet
  else
    info "Cloning clock into $CLONE_DIR..."
    git clone --depth 1 --branch "$BRANCH" "https://github.com/$REPO.git" "$CLONE_DIR" --quiet
    cd "$CLONE_DIR"
  fi

  success "Source ready at $CLONE_DIR"
}

# --- Build ---

build_tools() {
  cd "$CLONE_DIR"

  info "Building Go tools..."
  make go-tools 2>&1 | while IFS= read -r line; do
    if echo "$line" | grep -q "^go build"; then
      tool=$(echo "$line" | grep -oE '\./cmd/[a-z]+' | sed 's|./cmd/||')
      printf "\r  Building %-12s" "$tool"
    fi
  done
  echo ""
  success "Go tools built"

  info "Building shell tools..."
  make shell-tools >/dev/null 2>&1
  success "Shell tools built"

  local count
  count=$(ls bin/ 2>/dev/null | wc -l | tr -d ' ')
  success "$count tools built in bin/"
}

# --- Install ---

install_tools() {
  local needs_sudo=false

  if [ ! -w "$INSTALL_DIR" ]; then
    needs_sudo=true
    warn "$INSTALL_DIR not writable, will use sudo"
  fi

  info "Installing to $INSTALL_DIR..."

  cd "$CLONE_DIR"

  # Install the main clock binary
  if $needs_sudo; then
    sudo cp bin/clock "$INSTALL_DIR/clock"
    sudo chmod +x "$INSTALL_DIR/clock"
  else
    cp bin/clock "$INSTALL_DIR/clock"
    chmod +x "$INSTALL_DIR/clock"
  fi

  # Install all other tools as clock-<name> to avoid PATH conflicts
  for f in bin/*; do
    name=$(basename "$f")
    if [ "$name" = "clock" ]; then
      continue
    fi
    if $needs_sudo; then
      sudo cp "$f" "$INSTALL_DIR/clock-$name"
      sudo chmod +x "$INSTALL_DIR/clock-$name"
    else
      cp "$f" "$INSTALL_DIR/clock-$name"
      chmod +x "$INSTALL_DIR/clock-$name"
    fi
  done

  # Also symlink clock-* without prefix into a clock tools dir so the
  # orchestrator can find siblings. The clock binary finds tools by looking
  # next to itself first, so having them as clock-<name> in the same dir works
  # only if we also place un-prefixed copies. We place them in a dedicated dir.
  local tools_dir="$INSTALL_DIR/clock-tools"
  if $needs_sudo; then
    sudo mkdir -p "$tools_dir"
    for f in bin/*; do
      name=$(basename "$f")
      sudo cp "$f" "$tools_dir/$name"
      sudo chmod +x "$tools_dir/$name"
    done
  else
    mkdir -p "$tools_dir"
    for f in bin/*; do
      name=$(basename "$f")
      cp "$f" "$tools_dir/$name"
      chmod +x "$tools_dir/$name"
    done
  fi

  local tool_count
  tool_count=$(ls bin/ 2>/dev/null | wc -l | tr -d ' ')
  success "Installed $tool_count tools"
}

# --- Verify ---

verify_install() {
  echo ""
  info "Verifying installation..."

  if check_cmd "clock"; then
    local clock_path
    clock_path=$(command -v clock)
    success "clock binary: $clock_path"
  else
    error "clock not found in PATH"
    warn "Add $INSTALL_DIR to your PATH:"
    warn "  export PATH=\"$INSTALL_DIR:\$PATH\""
    return 1
  fi

  # Quick doctor check
  info "Running clock doctor..."
  clock doctor 2>/dev/null || true
  echo ""
}

# --- Shell config ---

print_shell_config() {
  local shell_name
  shell_name=$(basename "${SHELL:-/bin/bash}")

  local rc_file
  case "$shell_name" in
    zsh)  rc_file="$HOME/.zshrc" ;;
    bash) rc_file="$HOME/.bashrc" ;;
    fish) rc_file="$HOME/.config/fish/config.fish" ;;
    *)    rc_file="$HOME/.profile" ;;
  esac

  # Check if clock-tools is in PATH already
  if ! echo "$PATH" | grep -q "clock-tools"; then
    echo ""
    warn "Add clock-tools to your PATH for direct tool access:"
    echo ""
    if [ "$shell_name" = "fish" ]; then
      echo "  fish_add_path $INSTALL_DIR/clock-tools"
    else
      echo "  echo 'export PATH=\"$INSTALL_DIR/clock-tools:\$PATH\"' >> $rc_file"
      echo "  source $rc_file"
    fi
    echo ""
  fi
}

# --- Uninstall ---

uninstall() {
  info "Uninstalling Clock..."

  local needs_sudo=false
  if [ ! -w "$INSTALL_DIR" ]; then
    needs_sudo=true
  fi

  if $needs_sudo; then
    sudo rm -f "$INSTALL_DIR/clock"
    sudo rm -f "$INSTALL_DIR"/clock-*
    sudo rm -rf "$INSTALL_DIR/clock-tools"
  else
    rm -f "$INSTALL_DIR/clock"
    rm -f "$INSTALL_DIR"/clock-*
    rm -rf "$INSTALL_DIR/clock-tools"
  fi

  if [ -d "$CLONE_DIR" ]; then
    rm -rf "$CLONE_DIR"
    success "Removed source directory $CLONE_DIR"
  fi

  success "Clock uninstalled"
}

# --- Main ---

main() {
  header

  # Handle --uninstall flag
  if [ "${1:-}" = "--uninstall" ] || [ "${1:-}" = "uninstall" ]; then
    uninstall
    exit 0
  fi

  # Handle --help
  if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
    echo "Usage: install.sh [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --uninstall    Remove Clock from system"
    echo "  --help         Show this help"
    echo ""
    echo "Environment variables:"
    echo "  CLOCK_INSTALL_DIR  Installation directory (default: /usr/local/bin)"
    echo "  CLOCK_CLONE_DIR    Source clone directory (default: ~/.clock-src)"
    echo "  CLOCK_BRANCH       Git branch to install (default: main)"
    exit 0
  fi

  check_dependencies
  fetch_source
  build_tools
  install_tools
  verify_install
  print_shell_config

  echo -e "${BOLD}${GREEN}Clock installed successfully!${NC}"
  echo ""
  echo "  Get started:"
  echo "    cd /path/to/your/repo"
  echo "    clock init          # initialize project context"
  echo "    clock               # start interactive session"
  echo "    clock ask \"...\"     # ask questions"
  echo "    clock fix \"...\"     # fix issues"
  echo ""
}

main "$@"
