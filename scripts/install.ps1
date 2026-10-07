# Installs the latest whatsapp-mcp release into Claude Desktop.
#   irm https://raw.githubusercontent.com/nchdatta/whatsapp-mcp/main/scripts/install.ps1 | iex
# Pin a version by setting $env:WHATSAPP_MCP_VERSION = "v1.2.3" first.
$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = "nchdatta/whatsapp-mcp"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$asset = "whatsapp-mcp-windows-$arch.exe"
$base = if ($env:WHATSAPP_MCP_VERSION) {
    "https://github.com/$repo/releases/download/$($env:WHATSAPP_MCP_VERSION)"
} else {
    "https://github.com/$repo/releases/latest/download"
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("whatsapp-mcp-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $exe = Join-Path $tmp $asset
    Write-Host "Downloading $asset"
    $ProgressPreference = "SilentlyContinue" # makes Invoke-WebRequest much faster
    Invoke-WebRequest "$base/$asset" -OutFile $exe -UseBasicParsing
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile (Join-Path $tmp "SHA256SUMS") -UseBasicParsing

    $line = Get-Content (Join-Path $tmp "SHA256SUMS") | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))$" }
    if (-not $line) { throw "$asset is missing from SHA256SUMS" }
    $expected = ($line -split "\s+")[0].ToLower()
    $actual = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { throw "Checksum mismatch for $asset; aborting" }
    Write-Host "Checksum OK"

    & $exe install
    if ($LASTEXITCODE -ne 0) { throw "install failed" }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
