#!/usr/bin/env bash
# mirror-symbol-diff.sh compares the exported Go API surface of two
# directory trees (each its own module) and reports whether NEW removed or
# changed any exported symbol that OLD had. Purely additive changes exit 0;
# removed/changed symbols exit 1 so the caller can gate on the "breaking"
# label.
#
# Why not `apidiff -m OLD NEW`?
# golang.org/x/exp/cmd/apidiff's `-m` mode resolves OLD and NEW as *module
# paths* through the current module's build graph (go/packages), not as
# arbitrary directories. Two independently generated trees that declare the
# same module path (as the workers-go mirror always does) cannot both be
# loaded into one build graph at once; apidiff fails with:
#   "directory prefix ... does not contain main module or its selected
#   dependencies"
# (confirmed experimentally). So this script implements the documented
# fallback instead: dump each package's exported API with `go doc -all` and
# diff the text.
#
# Usage: mirror-symbol-diff.sh OLD_DIR NEW_DIR
set -euo pipefail

if [ $# -ne 2 ]; then
  echo "usage: $0 OLD_DIR NEW_DIR" >&2
  exit 2
fi

OLD_DIR=$(cd "$1" && pwd)
NEW_DIR=$(cd "$2" && pwd)

# dump_api DIR OUT_FILE writes a normalized dump of every exported symbol in
# every non-internal, non-main package of the module rooted at DIR to
# OUT_FILE.
dump_api() {
  local dir="$1"
  local out="$2"
  : > "$out"

  local pkgs
  pkgs=$(cd "$dir" && GOOS=js GOARCH=wasm go list ./... 2>/dev/null | grep -Ev '(^|/)internal(/|$)' || true)

  local pkg kind
  for pkg in $pkgs; do
    kind=$(cd "$dir" && GOOS=js GOARCH=wasm go list -f '{{.Name}}' "$pkg" 2>/dev/null || echo "")
    # "main" packages (commands, e.g. cmd/workers-assets-gen) export nothing
    # importable; skip them.
    if [ "$kind" = "main" ]; then
      continue
    fi

    echo "### $pkg" >> "$out"
    # `go doc -all` prints declarations unindented (or tab-indented, for
    # struct/interface bodies copied verbatim from source) and doc-comment
    # prose indented with spaces. Strip the prose so wording-only changes to
    # comments don't look like API changes; keep everything else.
    (cd "$dir" && GOOS=js GOARCH=wasm go doc -all "$pkg" 2>/dev/null) \
      | sed -E '/^ {4,}[^[:space:]]/d' \
      | sed '/^[[:space:]]*$/d' \
      >> "$out" || true
  done
}

OLD_API=$(mktemp)
NEW_API=$(mktemp)
DIFF_OUT=$(mktemp)
trap 'rm -f "$OLD_API" "$NEW_API" "$DIFF_OUT"' EXIT

dump_api "$OLD_DIR" "$OLD_API"
dump_api "$NEW_DIR" "$NEW_API"

if diff -u -B "$OLD_API" "$NEW_API" > "$DIFF_OUT"; then
  echo "No API changes detected."
  exit 0
fi

echo "API diff (old -> new):"
cat "$DIFF_OUT"

# Any removed line (a "-" line, excluding the "--- file" diff header) is a
# potential breaking change: a declaration that existed in OLD and does not
# appear verbatim in NEW. This covers both outright removals and
# signature/type changes (which show up as a paired -old/+new line).
removed=$(grep -E '^-[^-]' "$DIFF_OUT" || true)

if [ -n "$removed" ]; then
  echo
  echo "The following exported API lines were removed or changed:"
  echo "$removed"
  exit 1
fi

echo
echo "Only additive API changes detected."
exit 0
