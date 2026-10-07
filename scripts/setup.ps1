# Builds whatsapp-mcp and registers it with Claude Code.
# Usage (from the repo root):  powershell -ExecutionPolicy Bypass -File scripts\setup.ps1
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "bin\whatsapp-mcp.exe"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go is not installed: https://go.dev/dl/"
}

Write-Host "Building $exe"
Push-Location $root
try {
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w" -o $exe ./cmd/whatsapp-mcp
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

# Prefer claude on PATH; otherwise use the binary bundled with the VS Code extension
$claude = (Get-Command claude -ErrorAction SilentlyContinue).Source
if (-not $claude) {
    $bundled = "extensions\anthropic.claude-code-*\resources\native-binary\claude.exe"
    $claude = Get-ChildItem -Path (Join-Path $HOME ".vscode\$bundled"), (Join-Path $HOME ".vscode-insiders\$bundled") -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1 -ExpandProperty FullName
}
if (-not $claude) {
    Write-Host "Claude Code CLI not found. Add this to your MCP client config instead:"
    Write-Host "  command: $exe"
    Write-Host "  args:    [""serve""]"
    exit 0
}

# Windows PowerShell treats native stderr as an error under "Stop"; judge by exit code instead
$ErrorActionPreference = "Continue"
$existing = & $claude mcp get whatsapp 2>$null | Out-String
if ($LASTEXITCODE -eq 0) {
    if ($existing -like "*$exe*") {
        Write-Host "Already registered with Claude Code; the new build is used on its next start."
    } else {
        Write-Host "A different 'whatsapp' server is registered:"
        Write-Host $existing
        Write-Host "Replace it with:  claude mcp remove whatsapp; then run this script again."
    }
} else {
    & $claude mcp add --scope user whatsapp -- $exe serve
    if ($LASTEXITCODE -ne 0) { throw "claude mcp add failed" }
}

Write-Host ""
Write-Host "Done. Restart Claude Code and ask it to link your WhatsApp."
