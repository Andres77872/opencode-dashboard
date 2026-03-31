#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for cmd in npm go; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "error: $cmd is required but not installed" >&2
        exit 1
    fi
done

cd "$REPO_ROOT/web"
echo "===> Building frontend..."
npm install --silent
npm run build

echo "===> Preparing embed directory..."
rm -rf "$REPO_ROOT/internal/web/dist"
cp -r "$REPO_ROOT/web/dist" "$REPO_ROOT/internal/web/dist"

cd "$REPO_ROOT"
echo "===> Building Go binary with embedded assets..."
go build -tags embedassets -o opencode-dashboard ./cmd/opencode-dashboard

echo "===> Running opencode-dashboard..."
if [ $# -eq 0 ]; then
    exec ./opencode-dashboard web --port 7450
else
    exec ./opencode-dashboard web "$@"
fi