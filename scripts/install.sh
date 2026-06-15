#!/usr/bin/env bash
set -eu

REPO="${REPO:-Andres77872/opencode-dashboard}"
BINARY_NAME="${BINARY_NAME:-opencode-dashboard}"
VERSION="${VERSION:-latest}"
NO_CHECKSUM="${NO_CHECKSUM:-0}"
CONFIGURE_PATH="${CONFIGURE_PATH:-0}"

# --- output helpers (TTY-aware, NO_COLOR-aware) --------------------------------
# Colors are only emitted when stderr is a terminal and NO_COLOR is unset, so
# piped/redirected output (e.g. `opencode-dashboard update`, logs) stays plain.
if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
  c_reset=$(printf '\033[0m')
  c_bold=$(printf '\033[1m')
  c_red=$(printf '\033[31m')
  c_green=$(printf '\033[32m')
  c_yellow=$(printf '\033[33m')
  c_blue=$(printf '\033[34m')
else
  c_reset=""; c_bold=""; c_red=""; c_green=""; c_yellow=""; c_blue=""
fi

step_n=0
total_steps=6

log()  { printf '%s\n' "$*" >&2; }
info() { printf '%s\n' "$*" >&2; }
step() {
  step_n=$((step_n + 1))
  printf '%s[%d/%d]%s %s\n' "$c_blue$c_bold" "$step_n" "$total_steps" "$c_reset" "$*" >&2
}
success() { printf '%s\xe2\x9c\x93%s %s\n' "$c_green" "$c_reset" "$*" >&2; }
warn()    { printf '%swarning:%s %s\n' "$c_yellow" "$c_reset" "$*" >&2; }
err()     { printf '%serror:%s %s\n' "$c_red" "$c_reset" "$*" >&2; }
die()     { err "$*"; exit 1; }

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

# Returns the downloader to use ("curl" or "wget"), or dies.
http_tool() {
  if command -v curl >/dev/null 2>&1; then
    printf '%s\n' curl
  elif command -v wget >/dev/null 2>&1; then
    printf '%s\n' wget
  else
    die "need curl or wget"
  fi
}

# http_get URL [OUTFILE] [show_progress]
# When OUTFILE is empty, prints the body to stdout.
# When show_progress is "1" and stderr is a TTY, curl shows a progress bar.
http_get() {
  url="$1"
  out="${2:-}"
  show_progress="${3:-0}"
  tool=$(http_tool)

  if [ "$tool" = "curl" ]; then
    set -- -fL --retry 2
    if [ "$show_progress" = "1" ] && [ -t 2 ]; then
      set -- "$@" --progress-bar
    else
      set -- "$@" -sS
    fi
    if [ -n "${GITHUB_TOKEN:-}" ]; then
      set -- "$@" -H "Authorization: Bearer ${GITHUB_TOKEN}"
    fi
    if [ -n "$out" ]; then
      curl "$@" -o "$out" "$url"
    else
      curl "$@" "$url"
    fi
  else
    # wget
    set -- --tries=2
    if [ -n "${GITHUB_TOKEN:-}" ]; then
      set -- "$@" --header="Authorization: Bearer ${GITHUB_TOKEN}"
    fi
    if [ -n "$out" ]; then
      wget -q "$@" -O "$out" "$url"
    else
      wget -q "$@" -O- "$url"
    fi
  fi
}

parse_tag_name() {
  printf '%s' "$1" | tr -d '\n\r' \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

latest_tag() {
  api_url="https://api.github.com/repos/${REPO}/releases/latest"
  response=$(http_get "$api_url" "" 0) || {
    err "could not query the GitHub releases API"
    if [ -z "${GITHUB_TOKEN:-}" ]; then
      err "this is often GitHub API rate limiting; set GITHUB_TOKEN to a personal access token and retry"
    fi
    die "failed to resolve latest version"
  }
  parse_tag_name "$response"
}

pick_install_dir() {
  printf '%s\n' "$HOME/.local/bin"
}

# Portable file size in bytes (wc -c works on both GNU and BSD).
file_size() {
  wc -c < "$1" | tr -d ' '
}

# Human-readable size for display.
human_size() {
  bytes="$1"
  if [ "$bytes" -ge 1048576 ]; then
    awk -v b="$bytes" 'BEGIN { printf "%.1f MiB", b/1048576 }'
  elif [ "$bytes" -ge 1024 ]; then
    awk -v b="$bytes" 'BEGIN { printf "%.1f KiB", b/1024 }'
  else
    printf '%s B' "$bytes"
  fi
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

normalize_version() {
  printf '%s' "$1" | sed 's/^v//'
}

# Reads the installed binary's version string.
# Prefers `version --short` (machine readable, just "v0.1.20"); older binaries
# reject the flag and exit non-zero, so we fall back to parsing field 2 of
# `version` output: "opencode-dashboard v0.1.20 (abc123)".
get_installed_version() {
  binary_path="$1"
  if [ ! -x "$binary_path" ]; then
    printf '%s' ""
    return 0
  fi
  out=$("$binary_path" version --short 2>/dev/null || true)
  if [ -z "$out" ]; then
    out=$("$binary_path" version 2>/dev/null | awk 'NR==1{print $2}' || true)
  fi
  # Trim CR/LF, keep the first whitespace-delimited token, strip a leading "v".
  out=$(printf '%s' "$out" | tr -d '\r\n' | awk '{print $1}')
  printf '%s' "$out" | sed 's/^v//'
}

# Detect the shell rc file to suggest for PATH configuration.
detect_rc_file() {
  shell_name=$(basename "${SHELL:-}")
  case "$shell_name" in
    zsh)  printf '%s\n' "$HOME/.zshrc" ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then printf '%s\n' "$HOME/.bashrc"
      else printf '%s\n' "$HOME/.bash_profile"; fi ;;
    *)
      if [ -f "$HOME/.zshrc" ]; then printf '%s\n' "$HOME/.zshrc"
      elif [ -f "$HOME/.bashrc" ]; then printf '%s\n' "$HOME/.bashrc"
      else printf '%s\n' "$HOME/.profile"; fi ;;
  esac
}

# After install: report PATH status and, if requested, configure it.
handle_path() {
  dir="$1"
  case ":$PATH:" in
    *":$dir:"*)
      success "PATH already configured ($dir is on PATH)"
      return 0
      ;;
  esac

  export_line="export PATH=\"$dir:\$PATH\""
  rc_file=$(detect_rc_file)

  if [ "$CONFIGURE_PATH" = "1" ]; then
    # Idempotent append: only add if not already present.
    if [ -f "$rc_file" ] && grep -qF "$dir" "$rc_file" 2>/dev/null; then
      info "PATH entry already present in $rc_file"
    else
      printf '\n# Added by %s install script\n%s\n' "$BINARY_NAME" "$export_line" >> "$rc_file" \
        || die "failed to write PATH entry to $rc_file"
      success "Added $dir to PATH in $rc_file"
    fi
    info "Restart your shell or run: ${c_bold}source \"$rc_file\"${c_reset}"
  else
    warn "$dir is not on your PATH"
    info "Add this line to ${c_bold}$rc_file${c_reset}:"
    info "  ${c_bold}$export_line${c_reset}"
    info "Then restart your shell or run: source \"$rc_file\""
    info "Or re-run with ${c_bold}CONFIGURE_PATH=1${c_reset} to let this script append it for you."
  fi
}

main() {
  need_cmd_any curl wget
  need_cmd uname mktemp awk sed tar grep

  os=$(detect_os)
  arch=$(detect_arch)

  step "Resolving version"
  if [ "$VERSION" = "latest" ]; then
    tag=$(latest_tag)
    if [ -z "$tag" ]; then
      die "could not determine latest release tag"
    fi
    info "Latest release: ${c_bold}$tag${c_reset}"
  else
    tag="$VERSION"
    case "$tag" in
      v*) ;;
      *) tag="v$tag" ;;
    esac
    info "Requested version: ${c_bold}$tag${c_reset}"
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

  info "Current version: ${c_bold}${installed_version:-<not installed>}${c_reset}"
  info "Target version:  ${c_bold}$tag${c_reset}"
  info "Install path:    $binary_path"

  if [ -n "$normalized_installed" ] && [ "$normalized_installed" = "$normalized_target" ]; then
    success "Already at ${c_bold}$tag${c_reset} (nothing to do)"
    info "Location: $binary_path"
    exit 0
  fi

  archive_version=$(normalize_version "$tag")
  archive_name="${BINARY_NAME}_${archive_version}_${os}_${arch}.tar.gz"
  checksums_name="${BINARY_NAME}_${archive_version}_checksums.txt"
  base_url="https://github.com/${REPO}/releases/download/${tag}"
  archive_url="${base_url}/${archive_name}"
  checksums_url="${base_url}/${checksums_name}"

  workdir=$(mktemp -d)
  tmp_install=""
  # Clean up the work dir and any staged temp binary on any exit.
  trap 'rm -rf "$workdir"; [ -n "$tmp_install" ] && rm -f "$tmp_install" 2>/dev/null || true' EXIT HUP INT TERM

  step "Downloading $archive_name ($os/$arch)"
  if ! http_get "$archive_url" "$workdir/$archive_name" 1; then
    err "failed to download $archive_url"
    err "the release asset may not exist for $os/$arch, or the tag $tag may be missing (HTTP 404)"
    err "check available assets: https://github.com/${REPO}/releases/tag/${tag}"
    die "download failed"
  fi
  if [ ! -s "$workdir/$archive_name" ]; then
    die "downloaded archive is empty: $archive_url"
  fi
  arc_bytes=$(file_size "$workdir/$archive_name")
  info "Downloaded $(human_size "$arc_bytes")"

  if [ "$NO_CHECKSUM" != "1" ]; then
    step "Verifying checksum"
    need_cmd_any sha256sum shasum
    if ! http_get "$checksums_url" "$workdir/$checksums_name" 0; then
      die "failed to download checksums: $checksums_url (set NO_CHECKSUM=1 to skip)"
    fi
    if [ ! -s "$workdir/$checksums_name" ]; then
      die "downloaded checksums file is empty: $checksums_url"
    fi
    verify_checksum "$workdir/$archive_name" "$workdir/$checksums_name" \
      || die "checksum verification failed for $archive_name"
    success "Checksum verified"
  else
    step "Verifying checksum (skipped: NO_CHECKSUM=1)"
  fi

  step "Extracting"
  tar -xzf "$workdir/$archive_name" -C "$workdir" || die "failed to extract $archive_name"

  if [ -f "$workdir/$BINARY_NAME" ]; then
    binary="$workdir/$BINARY_NAME"
  else
    binary=$(find "$workdir" -name "$BINARY_NAME" -type f | head -n1)
    if [ -z "$binary" ]; then
      die "could not find $BINARY_NAME in archive"
    fi
  fi

  step "Installing to $install_dir"
  mkdir -p "$install_dir" || die "could not create $install_dir"

  # Atomic install: copy into a temp file in the *same* directory, chmod it,
  # then mv -f over the destination. rename() is atomic on the same filesystem
  # and is safe even when overwriting the currently running binary (the running
  # process keeps the old inode), which avoids ETXTBSY during self-update.
  tmp_install=$(mktemp "${install_dir}/.${BINARY_NAME}.XXXXXX") \
    || die "could not create temp file in $install_dir"
  cp "$binary" "$tmp_install" || die "failed to stage binary"
  chmod 0755 "$tmp_install" || die "failed to chmod staged binary"
  mv -f "$tmp_install" "$binary_path" || die "failed to install binary to $binary_path"
  tmp_install=""

  success "Installed: $binary_path"

  # Confirm the freshly installed version (best effort).
  new_version=$(get_installed_version "$binary_path")
  [ -z "$new_version" ] && new_version=$(normalize_version "$tag")

  step "Done"
  if [ -n "$installed_version" ]; then
    success "${c_bold}Updated v${normalized_installed} -> v${new_version}${c_reset}"
  else
    success "${c_bold}Installed v${new_version}${c_reset} (fresh install)"
  fi

  echo >&2
  handle_path "$install_dir"
  echo >&2
  info "Verify: ${c_bold}$BINARY_NAME version${c_reset}"
}

main "$@"
