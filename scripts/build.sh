#!/usr/bin/env bash
# Builds SheetDiver for Linux and macOS. CI runs this same script, so a local
# build produces exactly what a release ships.
#
# Usage:
#   scripts/build.sh                      desktop app for this machine → build/bin
#   scripts/build.sh --package            ...and release artifacts → dist/
#   scripts/build.sh --headless --package headless server binaries for every OS/arch → dist/
#
# Options:
#   -v, --version VERSION   version stamped into the binary (default: git describe)
#   -p, --platform OS/ARCH  linux/amd64, linux/arm64, darwin/universal, darwin/arm64, darwin/amd64
#                           (default: this machine; macOS defaults to universal)
#       --package           write release artifacts to dist/
#       --headless          build the no-webview binary (`sheetdiver serve`) instead
#       --targets "LIST"    headless targets (default: linux/amd64 linux/arm64 darwin/amd64
#                           darwin/arm64 windows/amd64 windows/arm64)
#   -h, --help
#
# Environment (macOS, optional): CODESIGN_IDENTITY signs the .app; with
# APPLE_ID, APPLE_TEAM_ID and APPLE_APP_PASSWORD it is also notarized.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)
WAILS_VERSION="v2.17.0"
APP="SheetDiver"
BIN="sheetdiver"
BUNDLE_ID="io.github.aitakemyjob.sheetdiver"

VERSION=""
PLATFORM=""
PACKAGE=0
HEADLESS=0
TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

# say/warn/die print status, warnings and fatal errors; need asserts a command exists.
say()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33mwarn:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required${2:+ ($2)}"; }
# usage prints the header comment of this script as help text.
usage() { sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

while [ $# -gt 0 ]; do
  case "$1" in
    -v|--version)  VERSION="$2"; shift 2 ;;
    -p|--platform) PLATFORM="$2"; shift 2 ;;
    --package)     PACKAGE=1; shift ;;
    --headless)    HEADLESS=1; shift ;;
    --targets)     TARGETS="$2"; shift 2 ;;
    -h|--help)     usage ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

[ -n "$VERSION" ] || VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS="-s -w -X main.version=$VERSION"
# Numeric x.y.z for OS metadata (Info.plist, Windows resources, packages).
NUMERIC=$(printf '%s' "${VERSION#v}" | sed -E 's/^([0-9]+\.[0-9]+\.[0-9]+).*/\1/')
printf '%s' "$NUMERIC" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || NUMERIC="0.0.0"
# Package version: pre-releases use "~" so they sort before the final release
# in apt and rpm (0.1.0~rc.2 < 0.1.0). Non-version builds fall back to 0.0.0.
if printf '%s' "${VERSION#v}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
  PKGVER=$(printf '%s' "${VERSION#v}" | tr '-' '~')
else
  PKGVER="$NUMERIC"
fi

need go "https://go.dev/dl"
need node "https://nodejs.org"
need npm

# frontend installs npm dependencies if needed and builds frontend/dist.
frontend() {
  if [ ! -d frontend/node_modules ]; then
    say "installing frontend dependencies"
    (cd frontend && npm ci)
  fi
  say "building frontend"
  (cd frontend && npm run build >/dev/null)
}

# host_platform prints the Wails platform for this machine (macOS → universal).
host_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; esac
  case "$os" in
    darwin) echo "darwin/universal" ;;
    linux)  echo "linux/$arch" ;;
    *) die "unsupported build host: $os (use scripts/build.ps1 on Windows)" ;;
  esac
}

# ---------------------------------------------------------------- headless

if [ "$HEADLESS" = 1 ]; then
  frontend
  mkdir -p dist build/bin
  for t in $TARGETS; do
    os=${t%/*}; arch=${t#*/}
    ext=""; [ "$os" = windows ] && ext=".exe"
    out="build/bin/headless/${os}_${arch}/${BIN}${ext}"
    say "headless $os/$arch"
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -tags nogui -trimpath -ldflags "$LDFLAGS" -o "$out" .
    if [ "$PACKAGE" = 1 ]; then
      name="${BIN}-headless_${os}_${arch}"
      cp LICENSE THIRD_PARTY_NOTICES.md "$(dirname "$out")/"
      if [ "$os" = windows ]; then
        (cd "$(dirname "$out")" && zip -q "$ROOT/dist/$name.zip" "${BIN}.exe" LICENSE THIRD_PARTY_NOTICES.md)
      else
        tar -C "$(dirname "$out")" -czf "dist/$name.tar.gz" "$BIN" LICENSE THIRD_PARTY_NOTICES.md
      fi
    fi
  done
  say "done: $( [ "$PACKAGE" = 1 ] && echo dist/ || echo build/bin/headless/ )"
  exit 0
fi

# ---------------------------------------------------------------- desktop

[ -n "$PLATFORM" ] || PLATFORM=$(host_platform)
OS=${PLATFORM%/*}
ARCH=${PLATFORM#*/}

TAGS=""
if [ "$OS" = linux ]; then
  need pkg-config
  pkg-config --exists gtk+-3.0 || die "GTK 3 headers missing: sudo apt install libgtk-3-dev (Fedora: gtk3-devel)"
  if pkg-config --exists webkit2gtk-4.1; then
    TAGS="-tags webkit2_41"
  elif ! pkg-config --exists webkit2gtk-4.0; then
    die "WebKitGTK headers missing: sudo apt install libwebkit2gtk-4.1-dev (Fedora: webkit2gtk4.1-devel)"
  fi
fi

WAILS=$(command -v wails || true)
if [ -z "$WAILS" ]; then
  say "installing Wails CLI $WAILS_VERSION"
  go install "github.com/wailsapp/wails/v2/cmd/wails@$WAILS_VERSION"
  WAILS="$(go env GOPATH)/bin/wails"
fi

# Stamp the version into wails.json for the build, then restore it.
cp wails.json wails.json.bak
trap 'mv -f wails.json.bak wails.json' EXIT
node -e '
  const fs = require("fs"); const p = "wails.json";
  const c = JSON.parse(fs.readFileSync(p, "utf8"));
  c.info.productVersion = process.argv[1];
  fs.writeFileSync(p, JSON.stringify(c, null, 2) + "\n");
' "$NUMERIC"

say "building $APP $VERSION for $PLATFORM"
# shellcheck disable=SC2086 # TAGS is intentionally word-split
"$WAILS" build -clean -platform "$PLATFORM" $TAGS -trimpath -ldflags "$LDFLAGS"

if [ "$OS" = linux ]; then
  # The stdio MCP server as a static console program, beside the app: the
  # Claude Desktop extension carries it, so Claude starts it directly
  # (no Node launcher, no GTK libraries).
  say "building the console MCP bridge for $PLATFORM"
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags "$LDFLAGS" -o "build/bin/${BIN}-mcp" ./cmd/sheetdiver-mcp
fi

if [ "$PACKAGE" = 0 ]; then
  say "done: build/bin"
  exit 0
fi

mkdir -p dist
case "$OS" in
  linux)
    stage=$(mktemp -d)
    cp "build/bin/$BIN" "build/bin/${BIN}-mcp" packaging/sheetdiver.desktop LICENSE THIRD_PARTY_NOTICES.md "$stage/"
    cp build/appicon.png "$stage/$BIN.png"
    tar -C "$stage" -czf "dist/${BIN}_linux_${ARCH}.tar.gz" .
    rm -rf "$stage"
    say "dist/${BIN}_linux_${ARCH}.tar.gz"
    # The Claude Desktop extension around the same bridge, for any install.
    go run ./scripts/mcpb -platform linux -bridge "build/bin/${BIN}-mcp" -version "$VERSION" -o "dist/${BIN}_linux_${ARCH}.mcpb" >/dev/null
    say "dist/${BIN}_linux_${ARCH}.mcpb"
    if command -v nfpm >/dev/null 2>&1; then
      for fmt in deb rpm; do
        VERSION="$PKGVER" ARCH="$ARCH" nfpm package -f packaging/nfpm.yaml -p "$fmt" -t "dist/${BIN}_linux_${ARCH}.$fmt" >/dev/null
        say "dist/${BIN}_linux_${ARCH}.$fmt"
      done
    else
      warn "nfpm not found; skipping .deb/.rpm (go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.0)"
    fi
    ;;
  darwin)
    app="build/bin/$APP.app"
    # Wails' template uses com.wails.<name>; give the app its own identity
    # (used by macOS for preferences and by code signing).
    /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $BUNDLE_ID" "$app/Contents/Info.plist"
    cp LICENSE THIRD_PARTY_NOTICES.md "$app/Contents/Resources/"
    if [ -n "${CODESIGN_IDENTITY:-}" ]; then
      say "signing with $CODESIGN_IDENTITY"
      codesign --force --deep --options runtime --timestamp --sign "$CODESIGN_IDENTITY" "$app"
    fi
    zip="dist/${BIN}_darwin_${ARCH}.zip"
    ditto -c -k --keepParent "$app" "$zip"
    if [ -n "${CODESIGN_IDENTITY:-}" ] && [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ] && [ -n "${APPLE_APP_PASSWORD:-}" ]; then
      say "notarizing"
      xcrun notarytool submit "$zip" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_PASSWORD" --wait
      xcrun stapler staple "$app"
      rm -f "$zip" && ditto -c -k --keepParent "$app" "$zip"
    fi
    say "$zip"
    ;;
esac
