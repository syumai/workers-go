#!/usr/bin/env bash
# apidiff.sh compares the exported Go API surface of two workers-go *source*
# trees (each its own module) using golang.org/x/exp/cmd/apidiff, and
# reports whether NEW removed or changed any exported symbol that OLD had.
#
# Why source trees, and not the generated mirror trees (syumai/workers)?
# The mirror consists entirely of transparent forwarders (`type T = src.T`,
# `var F = src.F`). A forwarder's declared type never changes even when the
# underlying source symbol's signature, method set, or struct fields do, so
# diffing the mirror's exported API (as the old mirror-symbol-diff.sh did,
# via `go doc -all` text diffing) only ever catches whole-symbol add/remove.
# Comparing the workers-go source trees directly with apidiff catches
# signature changes, removed/changed methods, and changed struct fields too.
#
# Why apidiff's export-file mode, and not `apidiff -m OLD NEW` directly?
# `-m` resolves OLD and NEW as *module paths* through the current module's
# build graph (go/packages), not as arbitrary directories. Two independently
# checked-out trees that declare the *same* module path (as any two
# workers-go trees do) cannot both be loaded into one build graph at once;
# apidiff fails with "directory prefix ... does not contain main module or
# its selected dependencies" (confirmed experimentally). apidiff's
# export-file mode sidesteps this: `apidiff -m -w FILE MODULE`, run with cwd
# inside that tree, writes the module's exported API to FILE without
# needing the other tree loaded at all. The two export files are then
# compared with an ordinary `apidiff -m OLD_FILE NEW_FILE`.
#
# The source packages import syscall/js, so apidiff (a host binary that
# nonetheless loads the target packages via go/packages) is run with
# GOOS=js GOARCH=wasm in the environment so those packages load at all.
#
# apidiff excludes internal/ packages by default (no -allow-internal is
# passed, intentionally: internal packages are not part of the public API
# this check gates).
#
# Main packages (commands, e.g. cmd/workers-assets-gen) are excluded from
# the verdict too, though NOT from apidiff's own module load: they are not
# importable API, and genforward never forwards a main package into the
# mirror (its own cmd/workers-assets-gen is a hand-written stub, not
# generated), so a command being added/removed/changed must not flip
# "breaking" for every such PR. This script collects each tree's main
# package import paths with `go list -f '{{if eq .Name "main"}}...'` and
# strips any apidiff message about them before deciding the verdict (see
# filter_report below).
#
# apidiff always exits 0, whether or not it found incompatible changes (its
# exit status only reflects whether it *ran*, e.g. bad export data exits
# non-zero). So "any changes" is decided by inspecting whether
# `apidiff -m -incompatible` printed anything on stdout (after main-package
# filtering), not by its exit code.
#
# apidiff message format (read from golang.org/x/exp/apidiff's source,
# report.go and messageset.go, rather than assumed): every Change is one
# self-contained "- <message>\n" line; there is no multi-line "package X:"
# heading with indented continuation lines. In module mode, a whole
# package add/remove prints as "- package <full/import/path>: removed" (or
# ": added"); a changed symbol inside a non-root package prints as
# "- ./<path/relative/to/module>.<Symbol>: <description>"; a changed symbol
# in the module's root package prints as "- <Symbol>: <description>" (no
# prefix at all, since object names are relative to the module root
# package). filter_report matches on these exact prefixes.
#
# Usage: apidiff.sh OLD_DIR NEW_DIR
#   OLD_DIR, NEW_DIR: paths to workers-go source trees (each must have a
#   go.mod at its root).
#
# Exit codes:
#   0  no incompatible changes (additive changes, or no changes at all)
#   1  NEW removed or changed an exported symbol that OLD had
#   2  usage error
#   3  OLD_DIR and NEW_DIR declare different module paths, so their exported
#      APIs are not comparable (this happens exactly once in workers-go's
#      history: v0.34.0 was `github.com/syumai/workers`, v0.35.0 renamed the
#      module to `github.com/syumai/workers-go`)
#
# APIDIFF_VERSION pins the version of golang.org/x/exp/cmd/apidiff that is
# `go install`ed to do the comparison. x/exp has no semver tags, so this is
# a pseudo-version. It defaults to the version resolved with:
#   go list -m golang.org/x/exp@latest
# at the time this script was last updated. To bump it, re-run that command
# and update the default below (or pass APIDIFF_VERSION in the environment
# to override without editing the script).
set -euo pipefail

: "${APIDIFF_VERSION:=v0.0.0-20260824195058-e88cd73687aa}"

if [ $# -ne 2 ]; then
  echo "usage: $0 OLD_DIR NEW_DIR" >&2
  exit 2
fi

OLD_DIR=$(cd "$1" && pwd)
NEW_DIR=$(cd "$2" && pwd)

module_path() {
  local dir="$1"
  if [ ! -f "$dir/go.mod" ]; then
    # `go list -m` does not fail in a directory with no go.mod: it falls
    # back to reporting the pseudo-module "command-line-arguments" with
    # exit 0. Check explicitly so a bad input directory is a clear error
    # instead of a confusing "module paths differ" report.
    echo "$dir has no go.mod; it is not a workers-go source tree." >&2
    exit 2
  fi
  (cd "$dir" && go list -m)
}

OLD_MODULE=$(module_path "$OLD_DIR")
NEW_MODULE=$(module_path "$NEW_DIR")

if [ "$OLD_MODULE" != "$NEW_MODULE" ]; then
  echo "OLD_DIR ($OLD_DIR) declares module $OLD_MODULE but NEW_DIR ($NEW_DIR) declares module $NEW_MODULE." >&2
  echo "Their exported APIs are not comparable (this is expected exactly once: v0.34.0 was github.com/syumai/workers, v0.35.0 renamed the module to github.com/syumai/workers-go). Skipping the API diff." >&2
  exit 3
fi

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

# Collect the import paths of every `package main` in each tree (commands
# are not part of the exported API; see the header comment). Union of both
# trees, since a command may exist in only one of them (e.g. it was
# removed/added/renamed between OLD and NEW).
main_packages() {
  local dir="$1"
  (cd "$dir" && GOOS=js GOARCH=wasm go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./...) | sed '/^$/d'
}

MAIN_PKGS="$WORK_DIR/main_pkgs"
{ main_packages "$OLD_DIR"; main_packages "$NEW_DIR"; } | sort -u > "$MAIN_PKGS"

# Build the exact-line and line-prefix exclusion lists derived from
# MAIN_PKGS, in the message formats described in the header comment above.
EXCLUDE_EXACT="$WORK_DIR/exclude_exact"
EXCLUDE_PREFIX="$WORK_DIR/exclude_prefix"
: > "$EXCLUDE_EXACT"
: > "$EXCLUDE_PREFIX"
while IFS= read -r pkg; do
  [ -z "$pkg" ] && continue
  echo "- package $pkg: removed" >> "$EXCLUDE_EXACT"
  echo "- package $pkg: added" >> "$EXCLUDE_EXACT"
  case "$pkg" in
    "$OLD_MODULE"/*)
      echo "- ./${pkg#"$OLD_MODULE"/}." >> "$EXCLUDE_PREFIX"
      ;;
    *)
      # pkg == OLD_MODULE itself: a main package at the module root. apidiff
      # would then print its changed symbols with no prefix at all (same as
      # any other root-package symbol), which this script cannot distinguish
      # from a real API change. Does not occur in workers-go today (the
      # module root is package `workers`, not `main`).
      echo "workers-go has no main package at its module root ($pkg); root-package command filtering is not implemented." >&2
      ;;
  esac
done < "$MAIN_PKGS"

# filter_report drops any apidiff message about an excluded main package
# from stdin, and also drops an "Incompatible changes:" / "Compatible
# changes:" section header that ends up with nothing left under it (a
# header is only ever emitted by the non "-incompatible" report; buffering
# it as "pending" and only printing it once a surviving message follows
# keeps the report clean when every message under a header was filtered
# out, without needing to know which header we're under).
filter_report() {
  awk -v exact="$EXCLUDE_EXACT" -v prefix="$EXCLUDE_PREFIX" '
    BEGIN {
      while ((getline line < exact) > 0) ex[line] = 1
      while ((getline line < prefix) > 0) { n++; pre[n] = line }
    }
    /^(Incompatible|Compatible) changes:$/ { pending = $0; next }
    {
      skip = ($0 in ex)
      if (!skip) {
        for (i = 1; i <= n; i++) {
          if (index($0, pre[i]) == 1) { skip = 1; break }
        }
      }
      if (!skip) {
        if (pending != "") { print pending; pending = "" }
        print
      }
    }
  '
}

# Install apidiff into a scratch GOBIN so this script never depends on (or
# pollutes) the caller's toolchain beyond what `go install` needs.
GOBIN="$WORK_DIR/gobin"
mkdir -p "$GOBIN"
echo "Installing golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}..." >&2
GOBIN="$GOBIN" go install "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}"
APIDIFF="$GOBIN/apidiff"

OLD_EXPORT="$WORK_DIR/old.export"
NEW_EXPORT="$WORK_DIR/new.export"

echo "Writing export data for $OLD_MODULE at $OLD_DIR..." >&2
(cd "$OLD_DIR" && GOOS=js GOARCH=wasm "$APIDIFF" -m -w "$OLD_EXPORT" "$OLD_MODULE")
[ -s "$OLD_EXPORT" ] || { echo "apidiff wrote no export data for OLD_DIR ($OLD_DIR); refusing to compare against an empty/missing export file." >&2; exit 1; }

echo "Writing export data for $NEW_MODULE at $NEW_DIR..." >&2
(cd "$NEW_DIR" && GOOS=js GOARCH=wasm "$APIDIFF" -m -w "$NEW_EXPORT" "$NEW_MODULE")
[ -s "$NEW_EXPORT" ] || { echo "apidiff wrote no export data for NEW_DIR ($NEW_DIR); refusing to compare against an empty/missing export file." >&2; exit 1; }

if [ -s "$MAIN_PKGS" ]; then
  echo
  echo "Excluding main packages (not part of the public API) from the verdict:"
  sed 's/^/  - /' "$MAIN_PKGS"
fi

echo
echo "=== apidiff report ($OLD_DIR -> $NEW_DIR) ==="
# Piping (rather than capturing into a variable) keeps stdout flowing
# through filter_report while stderr ("Ignoring internal package ..."
# notices) still reaches the terminal directly, unaffected by the pipe.
"$APIDIFF" -m "$OLD_EXPORT" "$NEW_EXPORT" | filter_report

# apidiff always exits 0 here regardless of what it found; the presence of
# incompatible changes is decided from -incompatible's (filtered) stdout
# instead. Command substitution only captures stdout, so the "Ignoring
# internal package ..." notices (stderr) still reach the terminal but do
# not affect this check.
incompatible=$("$APIDIFF" -m -incompatible "$OLD_EXPORT" "$NEW_EXPORT" | filter_report)

if [ -n "$incompatible" ]; then
  echo
  echo "=== Incompatible changes ==="
  echo "$incompatible"
  exit 1
fi

echo
echo "No incompatible changes detected."
exit 0
