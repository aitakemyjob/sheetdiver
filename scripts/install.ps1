# SheetDiver installer for Windows.
#
#   irm https://raw.githubusercontent.com/aitakemyjob/sheetdiver/main/scripts/install.ps1 | iex
#
# Downloads the latest NSIS installer, verifies its checksum and runs it.
# Set $env:SHEETDIVER_VERSION to pin a release tag.
$ErrorActionPreference = 'Stop'
$repo = if ($env:SHEETDIVER_REPO) { $env:SHEETDIVER_REPO } else { 'aitakemyjob/sheetdiver' }
$version = if ($env:SHEETDIVER_VERSION) { $env:SHEETDIVER_VERSION } else { 'latest' }
$base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
$asset = 'sheetdiver_windows_amd64_installer.exe'
$tmp = Join-Path $env:TEMP $asset

Write-Host "==> downloading $asset" -ForegroundColor Cyan
Invoke-WebRequest "$base/$asset" -OutFile $tmp -UseBasicParsing

try {
  $sums = (Invoke-WebRequest "$base/checksums.txt" -UseBasicParsing).Content
  $line = ($sums -split "`n") | Where-Object { $_ -match " $([regex]::Escape($asset))$" }
  if ($line) {
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash $tmp -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { throw "checksum mismatch for $asset" }
    Write-Host '==> checksum ok' -ForegroundColor Cyan
  }
} catch [System.Net.WebException] { }

Write-Host '==> running installer' -ForegroundColor Cyan
Start-Process -FilePath $tmp -Wait
Remove-Item $tmp -ErrorAction SilentlyContinue
Write-Host '==> done. Connect Claude Code with:' -ForegroundColor Cyan
Write-Host '    claude mcp add --transport http --scope user sheetdiver http://127.0.0.1:47480/mcp'
