#!/usr/bin/env sh
# Builds whatsapp-mcp and adds it to Claude Desktop.
# Usage (from anywhere):  sh scripts/setup.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
bin="$root/bin/whatsapp-mcp"

command -v go >/dev/null 2>&1 || { echo "Go is not installed: https://go.dev/dl/" >&2; exit 1; }

echo "Building $bin"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$bin" ./cmd/whatsapp-mcp)

"$bin" install
