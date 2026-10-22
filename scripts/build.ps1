<#
.SYNOPSIS
  Builds SheetDiver for Windows. CI runs this same script.

.EXAMPLE
  scripts\build.ps1                     # desktop app + console MCP bridge -> build\bin
  scripts\build.ps1 -Package            # ...plus installer, zip and Claude Desktop extension -> dist\
  scripts\build.ps1 -Arch arm64 -Package -Version v1.2.3

.NOTES
  Requires Go, Node.js/npm. The NSIS installer needs makensis
  (choco install nsis); without it only the portable zip is produced.

  The console MCP bridge (sheetdiver-mcp.exe) is signed with Authenticode
  when WINDOWS_CERT_PFX (a base64 .pfx) and WINDOWS_CERT_PASSWORD are set
  and signtool is installed (Windows SDK); otherwise it is left unsigned.
#>
param(
  [string]$Version = '',
  [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
  [switch]$Package
)
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')
$WailsVersion = 'v2.16.0'
$McpbVersion = '2.1.2'
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

# Signs a file with Authenticode when a certificate is configured.
function Sign($file) {
  if (-not $env:WINDOWS_CERT_PFX) { Write-Warning "no WINDOWS_CERT_PFX; $file is unsigned"; return }
  $signtool = (Get-Command signtool -ErrorAction SilentlyContinue).Source
  if (-not $signtool) {
    $signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" -ErrorAction SilentlyContinue |
      Sort-Object FullName -Descending | Select-Object -First 1 -ExpandProperty FullName
  }
  if (-not $signtool) { throw 'signtool is required to sign (install the Windows SDK)' }
  $pfx = Join-Path ([IO.Path]::GetTempPath()) "sheetdiver-$([guid]::NewGuid()).pfx"
  try {
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_CERT_PFX))
    & $signtool sign /fd sha256 /tr http://timestamp.digicert.com /td sha256 /f $pfx /p $env:WINDOWS_CERT_PASSWORD $file
    if ($LASTEXITCODE -ne 0) { throw "signing $file failed ($LASTEXITCODE)" }
  }
  finally {
    Remove-Item $pfx -Force -ErrorAction SilentlyContinue
  }
}

# Throws unless the executable is a console program (PE subsystem 3), which
# a stdio MCP client can run; a windowsgui build (2) can't serve its stdio.
function AssertConsole($file) {
  $b = [IO.File]::ReadAllBytes((Resolve-Path $file))
  $pe = [BitConverter]::ToInt32($b, 0x3C)
  $subsystem = [BitConverter]::ToUInt16($b, $pe + 0x5C)
  if ($subsystem -ne 3) { throw "$file is not a console program (subsystem $subsystem)" }
}

# The console MCP bridge, built first: the installer (build\windows\installer\
# project.nsi) packs it from build\mcp, which wails build -clean leaves alone.
$bridge = 'build\mcp\sheetdiver-mcp.exe'
New-Item -ItemType Directory -Force build\mcp | Out-Null
Say "building the console MCP bridge for windows/$Arch"
$goEnv = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
try {
  $env:GOOS = 'windows'; $env:GOARCH = $Arch; $env:CGO_ENABLED = '0'
  go build -trimpath -ldflags $ldflags -o $bridge ./cmd/sheetdiver-mcp
  if ($LASTEXITCODE -ne 0) { throw "bridge build failed ($LASTEXITCODE)" }
}
finally {
  # Put the caller's Go environment back for the Wails build.
  foreach ($k in $goEnv.Keys) {
    if ($goEnv[$k]) { Set-Item "env:$k" $goEnv[$k] } else { Remove-Item "env:$k" -ErrorAction SilentlyContinue }
  }
}
AssertConsole $bridge
Sign $bridge

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
# Beside the app, where Connect an agent looks for it.
Copy-Item $bridge build\bin\ -Force

if (-not $Package) { Say 'done: build\bin'; exit 0 }

New-Item -ItemType Directory -Force dist | Out-Null
$zip = "dist\${Bin}_windows_${Arch}.zip"
Compress-Archive -Path "build\bin\$Bin.exe", "build\bin\sheetdiver-mcp.exe", LICENSE, THIRD_PARTY_NOTICES.md -DestinationPath $zip -Force
Say $zip

# The Claude Desktop extension around the same bridge, checked with the
# mcpb CLI.
$mcpb = "dist\${Bin}_windows_${Arch}.mcpb"
go run ./scripts/mcpb -bridge $bridge -version $Version -o $mcpb
if ($LASTEXITCODE -ne 0) { throw "extension build failed ($LASTEXITCODE)" }
$unpacked = 'build\mcp\extension'
Remove-Item $unpacked -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item $mcpb build\mcp\extension.zip -Force # Expand-Archive wants .zip
Expand-Archive build\mcp\extension.zip -DestinationPath $unpacked -Force
npx -y "@anthropic-ai/mcpb@$McpbVersion" validate "$unpacked\manifest.json"
if ($LASTEXITCODE -ne 0) { throw "the extension's manifest is invalid ($LASTEXITCODE)" }
Say $mcpb
if ($nsis) {
  $installer = Get-ChildItem build\bin -Filter '*-installer.exe' | Select-Object -First 1
  if (-not $installer) { throw 'NSIS installer was not produced' }
  Move-Item $installer.FullName "dist\${Bin}_windows_${Arch}_installer.exe" -Force
  Say "dist\${Bin}_windows_${Arch}_installer.exe"
}
else {
  Write-Warning 'makensis not found; skipped the installer (choco install nsis)'
}
