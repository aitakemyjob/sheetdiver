#!/usr/bin/env sh
# Downloads the real-datasheet test corpus listed in
# testdata/corpus/manifest.json into $SHEETDIVER_CORPUS (default
# ~/.cache/sheetdiver/corpus). The PDFs are the manufacturers' copyright and
# are never committed. Entries marked "manual" can't be fetched by script;
# download them in a browser into that folder under the given file name.
#
#   scripts/corpus.sh && make test-corpus
set -eu

cd "$(dirname "$0")/.."
dir="${SHEETDIVER_CORPUS:-${XDG_CACHE_HOME:-$HOME/.cache}/sheetdiver/corpus}"
[ "$(uname -s)" = "Darwin" ] && [ -z "${SHEETDIVER_CORPUS:-}" ] && dir="$HOME/Library/Caches/sheetdiver/corpus"
mkdir -p "$dir"
ua="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

node -e '
const m = require("./testdata/corpus/manifest.json");
for (const d of m.datasheets) console.log([d.file, d.url || "-", d.sha256, d.manual ? "manual" : "auto"].join("\t"));
' | while IFS="$(printf '\t')" read -r file url sha mode; do
  dest="$dir/$file"
  if [ ! -s "$dest" ]; then
    if [ "$mode" = "manual" ] || [ "$url" = "-" ]; then
      echo "manual   $file  (download $url in a browser to $dest)"
      continue
    fi
    if ! curl -fsSL --max-time 120 -A "$ua" -o "$dest.tmp" "$url"; then
      echo "failed   $file  ($url)"
      rm -f "$dest.tmp"
      continue
    fi
    mv "$dest.tmp" "$dest"
  fi
  got=$(sha256sum "$dest" 2>/dev/null || shasum -a 256 "$dest")
  got=${got%% *}
  if [ "$got" = "$sha" ]; then echo "ok       $file"; else echo "revised  $file  (checksum differs: the manufacturer updated it)"; fi
done
echo "corpus: $dir"
