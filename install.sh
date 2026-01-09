#!/usr/bin/env bash
set -euo pipefail

REPO="finbarr/yolobox"
REPO_URL="https://github.com/${REPO}.git"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

say() {
  echo -e "$*"
}

success() {
  say "${GREEN}✓${NC} $*"
}

info() {
  say "${CYAN}→${NC} $*"
}

warn() {
  say "${YELLOW}⚠${NC} $*"
}

error() {
  say "${RED}✗${NC} $*"
}

detect_platform() {
  local os arch

  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os" in
    Linux)  os="linux" ;;
    Darwin) os="darwin" ;;
    *)
      error "Unsupported OS: $os"
      exit 1
      ;;
  esac

  case "$arch" in
    x86_64|amd64)  arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      error "Unsupported architecture: $arch"
      exit 1
      ;;
  esac

  echo "${os}-${arch}"
}

get_latest_release() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | \
    grep '"tag_name"' | \
    sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

download_binary() {
  local platform="$1"
  local version="$2"
  local bindir="$3"
  local url="https://github.com/${REPO}/releases/download/${version}/yolobox-${platform}"

  info "Downloading yolobox ${version} for ${platform}..."

  local tmp
  tmp="$(mktemp)"
  if curl -fsSL "$url" -o "$tmp"; then
    mkdir -p "$bindir"
    install -m 0755 "$tmp" "$bindir/yolobox"
    rm -f "$tmp"
    return 0
  else
    rm -f "$tmp"
    return 1
  fi
}

build_from_source() {
  local bindir="$1"

  if ! command -v go >/dev/null 2>&1; then
    return 1
  fi

  info "Building from source..."

  local repo_dir
  repo_dir="$(resolve_repo_dir)"

  cd "$repo_dir"
  go build -o yolobox ./cmd/yolobox
  mkdir -p "$bindir"
  install -m 0755 yolobox "$bindir/yolobox"

  # Clean up if we cloned to a temp dir
  if [[ "$repo_dir" == /tmp/* ]] || [[ "$repo_dir" == /var/folders/* ]]; then
    rm -rf "$repo_dir"
  fi

  return 0
}

resolve_repo_dir() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  if [ -f "$script_dir/go.mod" ] && [ -d "$script_dir/cmd/yolobox" ]; then
    echo "$script_dir"
    return 0
  fi

  # Clone the repo
  if ! command -v git >/dev/null 2>&1; then
    error "git is required to build from source"
    exit 1
  fi

  local tmp
  tmp="$(mktemp -d 2>/dev/null || mktemp -d -t yolobox)"
  git clone --depth=1 "$REPO_URL" "$tmp" >/dev/null 2>&1
  echo "$tmp"
}

main() {
  say ""
  say "${CYAN}${BOLD}  Installing yolobox...${NC}"
  say ""

  local prefix bindir
  prefix="${YOLOBOX_PREFIX:-$HOME/.local}"
  bindir="${YOLOBOX_BINDIR:-$prefix/bin}"

  local platform
  platform="$(detect_platform)"

  # Try downloading a pre-built binary first
  local version
  version="$(get_latest_release)"

  if [ -n "$version" ]; then
    if download_binary "$platform" "$version" "$bindir"; then
      success "Installed yolobox ${version} to ${BOLD}$bindir/yolobox${NC}"
      post_install "$bindir"
      return 0
    else
      warn "No pre-built binary available for ${platform}"
    fi
  else
    warn "Could not fetch latest release, trying to build from source"
  fi

  # Fall back to building from source
  if build_from_source "$bindir"; then
    success "Installed yolobox to ${BOLD}$bindir/yolobox${NC}"
    post_install "$bindir"
    return 0
  fi

  # Neither worked
  say ""
  error "Could not install yolobox"
  say ""
  say "  Options:"
  say "  1. Install Go (https://go.dev) and run this script again"
  say "  2. Download a binary manually from:"
  say "     ${CYAN}https://github.com/${REPO}/releases${NC}"
  say ""
  exit 1
}

post_install() {
  local bindir="$1"

  if ! command -v yolobox >/dev/null 2>&1; then
    say ""
    warn "Make sure ${BOLD}$bindir${NC} is in your PATH"
    say ""
    say "  Add this to your shell config:"
    say "  ${CYAN}export PATH=\"$bindir:\$PATH\"${NC}"
  fi

  say ""
  say "  ${BOLD}Quick start:${NC}"
  say "  ${CYAN}cd /path/to/your/project${NC}"
  say "  ${CYAN}yolobox${NC}"
  say ""
  say "  ${YELLOW}Let your AI go full send. Your home directory stays home.${NC}"
  say ""
}

main "$@"
