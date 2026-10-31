#!/usr/bin/env bash
# Says whether a change needs CI's builds and tests: reads the changed
# files' paths on stdin and prints "true", or "false" when they're all
# documentation. Markdown that the code reads isn't documentation: the
# third-party notices are embedded in the binary and checked against the
# dependencies, docs/MCP.md is checked against the MCP tools, and
# packaging/ holds licence texts the notices are built from.
#
#   git diff --name-only BASE...HEAD | scripts/ci-changes.sh
set -euo pipefail

code=false
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    THIRD_PARTY_NOTICES.md | docs/MCP.md | packaging/*) code=true ;;
    *.md | docs/*) ;;
    *) code=true ;;
  esac
done
echo "$code"
