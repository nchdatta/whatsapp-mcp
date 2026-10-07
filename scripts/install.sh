#!/usr/bin/env sh
# Installs the latest whatsapp-mcp release into Claude Desktop.
#   curl -fsSL https://raw.githubusercontent.com/nchdatta/whatsapp-mcp/main/scripts/install.sh | sh
# Pin a version with: ... | WHATSAPP_MCP_VERSION=v1.2.3 sh
set -eu

repo="nchdatta/whatsapp-mcp"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Unsupported OS: $(uname -s). On Windows use install.ps1." >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
asset="whatsapp-mcp-$os-$arch"

if [ -n "${WHATSAPP_MCP_VERSION:-}" ]; then
  base="https://github.com/$repo/releases/download/$WHATSAPP_MCP_VERSION"
else
  base="https://github.com/$repo/releases/latest/download"
fi

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else echo "Need curl or wget" >&2; exit 1
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $asset"
fetch "$base/$asset" "$tmp/$asset"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"

expected=$(grep " \*\{0,1\}$asset\$" "$tmp/SHA256SUMS" | awk '{print $1}')
[ -n "$expected" ] || { echo "$asset is missing from SHA256SUMS" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || { echo "Checksum mismatch for $asset; aborting" >&2; exit 1; }
echo "Checksum OK"

chmod +x "$tmp/$asset"
if [ -t 1 ] && (exec </dev/tty) 2>/dev/null; then
  # Under "curl | sh" stdin is the script itself; take answers from the terminal
  "$tmp/$asset" install </dev/tty
else
  "$tmp/$asset" install
fi

case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) echo "Tip: add ~/.local/bin to your PATH to run whatsapp-mcp from a terminal." ;;
esac
