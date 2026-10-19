#!/usr/bin/env sh
# SheetDiver installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/aitakemyjob/sheetdiver/main/scripts/install.sh | sh
#
# Environment:
#   SHEETDIVER_VERSION  release tag to install (default: latest)
#   SHEETDIVER_REPO     GitHub repo (default: aitakemyjob/sheetdiver)
#   PREFIX            where the `sheetdiver` command goes (default: ~/.local)
set -eu

REPO="${SHEETDIVER_REPO:-aitakemyjob/sheetdiver}"
VERSION="${SHEETDIVER_VERSION:-latest}"
PREFIX="${PREFIX:-$HOME/.local}"

say() { printf '\033[36m==>\033[0m %s\n' "$*"; }
die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

need curl
need uname

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() {
  say "downloading $1"
  curl -fL --progress-bar "$base/$1" -o "$tmp/$1" || die "download failed: $base/$1"
  if curl -fsL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
    expected=$(grep " $1\$" "$tmp/checksums.txt" | awk '{print $1}')
    if [ -n "$expected" ]; then
      if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$tmp/$1" | awk '{print $1}');
      else actual=$(shasum -a 256 "$tmp/$1" | awk '{print $1}'); fi
      [ "$expected" = "$actual" ] || die "checksum mismatch for $1"
      say "checksum ok"
    fi
  fi
}

case "$os" in
  linux)
    asset="sheetdiver_linux_${arch}.tar.gz"
    fetch "$asset"
    tar -xzf "$tmp/$asset" -C "$tmp"
    mkdir -p "$PREFIX/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/512x512/apps"
    install -m 0755 "$tmp/sheetdiver" "$PREFIX/bin/sheetdiver"
    install -m 0644 "$tmp/sheetdiver.png" "$HOME/.local/share/icons/hicolor/512x512/apps/sheetdiver.png"
    sed "s|^Exec=.*|Exec=$PREFIX/bin/sheetdiver|" "$tmp/sheetdiver.desktop" > "$HOME/.local/share/applications/sheetdiver.desktop"
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$HOME/.local/share/applications" || true
    if ! ldconfig -p 2>/dev/null | grep -q libwebkit2gtk-4.1; then
      say "note: SheetDiver needs WebKitGTK. On Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0"
    fi
    ;;
  darwin)
    asset="sheetdiver_darwin_universal.zip"
    fetch "$asset"
    need unzip
    unzip -q "$tmp/$asset" -d "$tmp"
    dest="/Applications"
    [ -w "$dest" ] || dest="$HOME/Applications"
    mkdir -p "$dest"
    rm -rf "$dest/SheetDiver.app"
    mv "$tmp/SheetDiver.app" "$dest/"
    # Release builds are not notarized yet; clear the quarantine flag so
    # Gatekeeper lets the app open.
    xattr -dr com.apple.quarantine "$dest/SheetDiver.app" 2>/dev/null || true
    mkdir -p "$PREFIX/bin"
    ln -sf "$dest/SheetDiver.app/Contents/MacOS/sheetdiver" "$PREFIX/bin/sheetdiver"
    say "installed $dest/SheetDiver.app"
    ;;
  *)
    die "unsupported OS: $os (on Windows use scripts/install.ps1 or the installer .exe)"
    ;;
esac

say "installed $PREFIX/bin/sheetdiver"
case ":$PATH:" in
  *":$PREFIX/bin:"*) ;;
  *) say "add $PREFIX/bin to your PATH to use the \`sheetdiver\` command" ;;
esac
say "connect Claude Code:  claude mcp add --transport http --scope user sheetdiver http://127.0.0.1:47480/mcp"
