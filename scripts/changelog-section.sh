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
# Trim leading and trailing blank lines.
printf '%s\n' "$body" | awk 'NF { started = 1 } started' | awk '{ lines[NR] = $0 } END { n = NR; while (n > 0 && lines[n] ~ /^[[:space:]]*$/) n--; for (i = 1; i <= n; i++) print lines[i] }'
