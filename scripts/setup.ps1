# Builds whatsapp-mcp and adds it to Claude Desktop.
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
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed. If Claude Desktop is running it holds the exe open: quit it and try again."
    }
} finally {
    Pop-Location
}

& $exe install
if ($LASTEXITCODE -ne 0) { throw "install failed" }
