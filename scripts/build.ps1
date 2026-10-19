<#
.SYNOPSIS
  Builds SheetDiver for Windows. CI runs this same script.

.EXAMPLE
  scripts\build.ps1                     # desktop app -> build\bin
  scripts\build.ps1 -Package            # ...plus installer and zip -> dist\
  scripts\build.ps1 -Arch arm64 -Package -Version v1.2.3

.NOTES
  Requires Go, Node.js/npm. The NSIS installer needs makensis
  (choco install nsis); without it only the portable zip is produced.
#>
param(
  [string]$Version = '',
  [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
  [switch]$Package
)
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')
$WailsVersion = 'v2.16.0'
$App = 'SheetDiver'
$Bin = 'sheetdiver'

function Say($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Need($cmd, $hint) { if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) { throw "$cmd is required ($hint)" } }

Need go 'https://go.dev/dl'
Need node 'https://nodejs.org'
Need npm 'https://nodejs.org'

if (-not $Version) {
  $Version = (git describe --tags --always --dirty 2>$null)
  if (-not $Version) { $Version = 'dev' }
}
$numeric = '0.0.0'
if ($Version.TrimStart('v') -match '^(\d+\.\d+\.\d+)') { $numeric = $Matches[1] }
$ldflags = "-s -w -X main.version=$Version"

$wails = (Get-Command wails -ErrorAction SilentlyContinue).Source
if (-not $wails) {
  Say "installing Wails CLI $WailsVersion"
  go install "github.com/wailsapp/wails/v2/cmd/wails@$WailsVersion"
  $wails = Join-Path (go env GOPATH) 'bin\wails.exe'
}

$nsis = [bool](Get-Command makensis -ErrorAction SilentlyContinue)
if (-not $nsis -and (Test-Path 'C:\Program Files (x86)\NSIS\makensis.exe')) {
  $env:PATH += ';C:\Program Files (x86)\NSIS'
  $nsis = $true
}

# Stamp the version into wails.json for the build, then restore it.
Copy-Item wails.json wails.json.bak -Force
try {
  $cfg = Get-Content wails.json -Raw | ConvertFrom-Json
  $cfg.info.productVersion = $numeric
  $cfg | ConvertTo-Json -Depth 10 | Set-Content wails.json -Encoding utf8

  Say "building $App $Version for windows/$Arch"
  $wailsArgs = @('build', '-clean', '-platform', "windows/$Arch", '-trimpath', '-ldflags', $ldflags)
  if ($Package -and $nsis) { $wailsArgs += '-nsis' }
  & $wails @wailsArgs
  if ($LASTEXITCODE -ne 0) { throw "wails build failed ($LASTEXITCODE)" }
}
finally {
  Move-Item wails.json.bak wails.json -Force
}

if (-not $Package) { Say 'done: build\bin'; exit 0 }

New-Item -ItemType Directory -Force dist | Out-Null
$zip = "dist\${Bin}_windows_${Arch}.zip"
Compress-Archive -Path "build\bin\$Bin.exe" -DestinationPath $zip -Force
Say $zip
if ($nsis) {
  $installer = Get-ChildItem build\bin -Filter '*-installer.exe' | Select-Object -First 1
  if (-not $installer) { throw 'NSIS installer was not produced' }
  Move-Item $installer.FullName "dist\${Bin}_windows_${Arch}_installer.exe" -Force
  Say "dist\${Bin}_windows_${Arch}_installer.exe"
}
else {
  Write-Warning 'makensis not found; skipped the installer (choco install nsis)'
}
