#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-$REPO_ROOT/build}"
OUTPUT="${OUTPUT:-$BUILD_DIR/opencode-dashboard}"
VERSION="${VERSION:-}"
COMMIT="${COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "unknown")}"

for cmd in npm go; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "error: $cmd is required but not installed" >&2
        exit 1
    fi
done

mkdir -p "$BUILD_DIR"

cd "$REPO_ROOT/web"
echo "===> Installing frontend dependencies..."
npm ci --silent

echo "===> Building frontend..."
npm run build

echo "===> Preparing embed directory..."
rm -rf "$REPO_ROOT/internal/web/dist"
cp -r "$REPO_ROOT/web/dist" "$REPO_ROOT/internal/web/dist"

cd "$REPO_ROOT"
echo "===> Building Go binary with embedded assets..."

LDFLAGS="-s -w"
if [ -n "$VERSION" ]; then
    LDFLAGS="$LDFLAGS -X 'opencode-dashboard/internal/version.Version=$VERSION'"
fi
if [ -n "$COMMIT" ]; then
    LDFLAGS="$LDFLAGS -X 'opencode-dashboard/internal/version.GitCommit=$COMMIT'"
fi

go build -tags embedassets -ldflags "$LDFLAGS" -o "$OUTPUT" ./cmd/opencode-dashboard

echo "===> Build complete: $OUTPUT"