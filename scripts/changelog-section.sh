#!/bin/sh
# Prints the CHANGELOG section for a release, for the top of its release
# notes. For v1.2.3 or v1.2.3-rc.1 it uses "## [1.2.3]" if the changelog
# has it, otherwise "## [Unreleased]" (release candidates are often cut
# before the section is renamed). Prints nothing if neither exists.
#
#   scripts/changelog-section.sh v0.2.0-rc.1 [CHANGELOG.md]
set -eu
tag=${1:?usage: changelog-section.sh <tag> [changelog]}
file=${2:-CHANGELOG.md}
version=$(printf '%s' "$tag" | sed -E 's/^v//; s/-.*$//')

section() {
  awk -v head="$1" '
    index($0, head) == 1 { found = 1; next }
    found && /^## \[/ { exit }
    found { print }
  ' "$file"
}

body=$(section "## [$version]")
[ -n "$(printf '%s' "$body" | tr -d '[:space:]')" ] || body=$(section "## [Unreleased]")
# Trim leading and trailing blank lines, and join hard-wrapped lines: GitHub
# renders every newline in release notes as a line break, so a paragraph or
# list item wrapped at 72 columns in the changelog would break mid-sentence.
printf '%s\n' "$body" | awk 'NF { started = 1 } started' | awk '{ lines[NR] = $0 } END { n = NR; while (n > 0 && lines[n] ~ /^[[:space:]]*$/) n--; for (i = 1; i <= n; i++) print lines[i] }' | awk '
  # A line starts a new block when it is blank, a heading, a list item,
  # a quote, a table row or a fence; otherwise it continues the last one.
  function starts(l) { return l ~ /^[[:space:]]*$/ || l ~ /^#/ || l ~ /^[[:space:]]*([-*+]|[0-9]+\.)[[:space:]]/ || l ~ /^>/ || l ~ /^[[:space:]]*\|/ || l ~ /^[[:space:]]*```/ }
  /^[[:space:]]*```/ { if (cur != "") print cur; cur = ""; print; fence = !fence; next }
  fence { print; next }
  {
    if (starts($0) || cur == "") { if (cur != "") print cur; cur = $0; if ($0 ~ /^[[:space:]]*$/ || $0 ~ /^#/) { print cur; cur = "" }; next }
    line = $0; sub(/^[[:space:]]+/, "", line); cur = cur " " line
  }
  END { if (cur != "") print cur }
'
