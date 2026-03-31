#!/usr/bin/env bash
set -eu

REPO="${REPO:-Andres77872/opencode-dashboard}"
BINARY_NAME="${BINARY_NAME:-opencode-dashboard}"
VERSION="${VERSION:-latest}"
NO_CHECKSUM="${NO_CHECKSUM:-0}"

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "need $1 but it's not available"
}

need_cmd_any() {
  for cmd in "$@"; do
    if command -v "$cmd" >/dev/null 2>&1; then
      return 0
    fi
  done
  die "need one of $* but none are available"
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf '%s\n' linux ;;
    Darwin) printf '%s\n' darwin ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s\n' amd64 ;;
    arm64|aarch64) printf '%s\n' arm64 ;;
    *) die "unsupported arch: $(uname -m)" ;;
  esac
}

http_get() {
  url="$1"
  out="${2:-}"
  auth_header=""
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    auth_header="-H \"Authorization: Bearer ${GITHUB_TOKEN}\""
  fi
  if command -v curl >/dev/null 2>&1; then
    if [ -n "$out" ]; then
      eval curl -fsSL "$auth_header" -o "\"\$out\"" "\"\$url\""
    else
      eval curl -fsSL "$auth_header" "\"\$url\""
    fi
  elif command -v wget >/dev/null 2>&1; then
    if [ -n "$out" ]; then
      eval wget -qO "\"\$out\"" "$auth_header" "\"\$url\""
    else
      eval wget -qO- "$auth_header" "\"\$url\""
    fi
  else
    die "need curl or wget"
  fi
}

parse_tag_name() {
  printf '%s' "$1" | tr -d '\n\r' \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

latest_tag() {
  api_url="https://api.github.com/repos/${REPO}/releases/latest"
  response=$(http_get "$api_url")
  parse_tag_name "$response"
}

pick_install_dir() {
  printf '%s\n' "$HOME/.local/bin"
}

verify_checksum() {
  archive="$1"
  checksums_file="$2"
  archive_name=$(basename "$archive")
  line=$(awk -v f="$archive_name" '$2==f||$2=="./"f{print;exit}' "$checksums_file")
  if [ -z "$line" ]; then
    die "no checksum entry for $archive_name"
  fi
  cd "$(dirname "$archive")"
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s\n' "$line" | sha256sum -c - >/dev/null 2>&1
  elif command -v shasum >/dev/null 2>&1; then
    printf '%s\n' "$line" | shasum -a 256 -c - >/dev/null 2>&1
  else
    die "need sha256sum or shasum for checksum verification"
  fi
}

print_path_hint() {
  dir="$1"
  case ":$PATH:" in
    *":$dir:"*) ;;
    *) log "warning: $dir is not in PATH"; log "  Add: export PATH=\"$dir:\$PATH\"" ;;
  esac
}

normalize_version() {
  printf '%s' "$1" | sed 's/^v//'
}

get_installed_version() {
  binary_path="$1"
  if [ -x "$binary_path" ]; then
    "$binary_path" version 2>/dev/null | cut -d' ' -f1 | sed 's/^v//' || echo ""
  else
    echo ""
  fi
}

main() {
  need_cmd_any curl wget
  need_cmd uname mktemp awk sed tar cut

  os=$(detect_os)
  arch=$(detect_arch)

  if [ "$VERSION" = "latest" ]; then
    tag=$(latest_tag)
    if [ -z "$tag" ]; then
      die "could not determine latest release tag"
    fi
  else
    tag="$VERSION"
    case "$tag" in
      v*) ;;
      *) tag="v$tag" ;;
    esac
  fi

  install_dir=$(pick_install_dir)
  binary_path="${install_dir}/${BINARY_NAME}"

  installed_version=""
  if [ -x "$binary_path" ]; then
    installed_version=$(get_installed_version "$binary_path")
  fi

  normalized_installed=""
  if [ -n "$installed_version" ]; then
    normalized_installed=$(normalize_version "$installed_version")
  fi
  normalized_target=$(normalize_version "$tag")

  log "Current version: ${installed_version:-<not installed>}"
  log "Target version:  $tag"
  log "Install path:    $binary_path"

  if [ -n "$normalized_installed" ] && [ "$normalized_installed" = "$normalized_target" ]; then
    log ""
    log "Action: SKIP (versions match)"
    log "Already installed at version $tag"
    log "Location: $binary_path"
    exit 0
  fi

  log ""
  if [ -n "$installed_version" ]; then
    log "Action: INSTALL (current '$installed_version' differs from target '$tag')"
  else
    log "Action: INSTALL (not currently installed)"
  fi

  log "Installing $BINARY_NAME $tag ($os/$arch)"

  archive_version=$(normalize_version "$tag")
  archive_name="${BINARY_NAME}_${archive_version}_${os}_${arch}.tar.gz"
  checksums_name="${BINARY_NAME}_${archive_version}_checksums.txt"

  base_url="https://github.com/${REPO}/releases/download/${tag}"

  workdir=$(mktemp -d)
  trap 'rm -rf "$workdir"' EXIT HUP INT TERM

  archive_url="${base_url}/${archive_name}"
  checksums_url="${base_url}/${checksums_name}"

  http_get "$archive_url" "$workdir/$archive_name"

  if [ "$NO_CHECKSUM" != "1" ]; then
    need_cmd_any sha256sum shasum
    http_get "$checksums_url" "$workdir/$checksums_name"
    verify_checksum "$workdir/$archive_name" "$workdir/$checksums_name"
    log "Checksum verified"
  fi

  mkdir -p "$install_dir"

  tar -xzf "$workdir/$archive_name" -C "$workdir"

  if [ -f "$workdir/$BINARY_NAME" ]; then
    binary="$workdir/$BINARY_NAME"
  else
    binary=$(find "$workdir" -name "$BINARY_NAME" -type f | head -n1)
    if [ -z "$binary" ]; then
      die "could not find $BINARY_NAME in archive"
    fi
  fi

  cp "$binary" "$install_dir/$BINARY_NAME"
  chmod 0755 "$install_dir/$BINARY_NAME"

  log ""
  log "Installed: $install_dir/$BINARY_NAME"
  log ""
  print_path_hint "$install_dir"
  log ""
  log "Verify: $BINARY_NAME version"
}

main "$@"