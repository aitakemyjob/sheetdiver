#!/usr/bin/env sh
# Updates a headless SheetDiver server from source: pulls the branch this
# checkout is on, builds the frontend and the headless binary, installs it
# over the old one and restarts the pm2 process that runs it, if any.
#
#   scripts/headless-update.sh                  # pull, build, install, restart
#   scripts/headless-update.sh --no-pull        # build what's checked out
#   scripts/headless-update.sh --dest /opt/sheetdiver/sheetdiver --pm2 sheets
#
# Needs Go 1.26+ and Node 22+. The binary goes to --dest (default
# ~/.local/bin/sheetdiver-headless, or $SHEETDIVER_BIN) and is swapped in
# with a rename, so a running server keeps its old file until pm2 restarts
# it. --pm2 NAME names the pm2 process to restart (default: sheetdiver,
# restarted only when pm2 is installed and the process exists).
set -eu

cd "$(dirname "$0")/.."
dest="${SHEETDIVER_BIN:-$HOME/.local/bin/sheetdiver-headless}"
pm2name=sheetdiver
pull=1
while [ $# -gt 0 ]; do
	case "$1" in
	--no-pull) pull=0 ;;
	--dest) dest="$2"; shift ;;
	--pm2) pm2name="$2"; shift ;;
	-h | --help) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "headless-update: unknown option $1" >&2; exit 2 ;;
	esac
	shift
done

if [ "$pull" = 1 ]; then
	branch="$(git rev-parse --abbrev-ref HEAD)"
	echo "==> git pull --ff-only ($branch)"
	git pull --ff-only
fi
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

echo "==> frontend"
cd frontend
if [ ! -d node_modules ] || [ package-lock.json -nt node_modules ]; then
	npm ci --no-audit --no-fund
fi
npm run build
cd ..

echo "==> go build ($version)"
mkdir -p "$(dirname "$dest")"
tmp="$(mktemp "$(dirname "$dest")/.sheetdiver-headless.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags "-s -w -X main.version=$version" -o "$tmp" .
mv -f "$tmp" "$dest"
chmod 755 "$dest"
trap - EXIT
echo "==> installed $("$dest" version) at $dest"

if command -v pm2 >/dev/null 2>&1 && pm2 describe "$pm2name" >/dev/null 2>&1; then
	echo "==> pm2 restart $pm2name"
	pm2 restart "$pm2name" --update-env
	pm2 logs "$pm2name" --lines 4 --nostream
else
	echo "==> no pm2 process '$pm2name' to restart"
fi
