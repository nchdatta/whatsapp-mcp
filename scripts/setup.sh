#!/usr/bin/env sh
# Builds whatsapp-mcp and registers it with Claude Code.
# Usage (from anywhere):  sh scripts/setup.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
bin="$root/bin/whatsapp-mcp"

command -v go >/dev/null 2>&1 || { echo "Go is not installed: https://go.dev/dl/" >&2; exit 1; }

echo "Building $bin"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$bin" ./cmd/whatsapp-mcp)

# Prefer claude on PATH; otherwise use the binary bundled with the VS Code extension
claude=$(command -v claude 2>/dev/null || true)
if [ -z "$claude" ]; then
  claude=$(ls -t "$HOME"/.vscode/extensions/anthropic.claude-code-*/resources/native-binary/claude "$HOME"/.vscode-insiders/extensions/anthropic.claude-code-*/resources/native-binary/claude 2>/dev/null | head -n 1 || true)
fi
if [ -z "$claude" ]; then
  echo "Claude Code CLI not found. Add this to your MCP client config instead:"
  echo "  command: $bin"
  echo '  args:    ["serve"]'
  exit 0
fi

if existing=$("$claude" mcp get whatsapp 2>/dev/null); then
  case "$existing" in
    *"$bin"*) echo "Already registered with Claude Code; the new build is used on its next start." ;;
    *) printf 'A different "whatsapp" server is registered:
%s
Replace it with: claude mcp remove whatsapp; then run this script again.
' "$existing" ;;
  esac
else
  "$claude" mcp add --scope user whatsapp -- "$bin" serve
fi

echo
echo "Done. Start Claude Code and ask it to link your WhatsApp."
