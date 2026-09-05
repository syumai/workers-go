#!/usr/bin/env bash
# mirror-tests/run.sh generates a mirror module with internal/cmd/genforward
# and verifies it, per the design in
# https://github.com/syumai/workers/issues/173 ("Design of the mirror").
#
# It must be run from anywhere inside the repository; it resolves the repo
# root itself.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

# The source module path is never hardcoded: it's read from go.mod so this
# script keeps working unchanged across the github.com/syumai/workers ->
# github.com/syumai/workers-go rename tracked by issue #173.
src_module="$(awk '/^module[ \t]/{print $2; exit}' go.mod)"
if [ -z "$src_module" ]; then
  echo "could not read module path from $repo_root/go.mod" >&2
  exit 1
fi

# A mirror module path distinct from the source module is required while the
# source module is still at the old github.com/syumai/workers path (a mirror
# can't both require and be replaced under the same import path). Once the
# rename in #173 lands and the source module becomes github.com/syumai/
# workers-go, the old path is free again and becomes the mirror module, which
# is also what production sync.yml will use.
mirror_module="github.com/syumai/workers"
if [ "$src_module" = "$mirror_module" ]; then
  mirror_module="github.com/syumai/workers-mirrortest"
fi

echo "source module: $src_module"
echo "mirror module (this run): $mirror_module"

tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/workers-mirror-tests.XXXXXX")"
cleanup() { rm -rf "$tmp_root"; }
trap cleanup EXIT

tmp_mirror="$tmp_root/mirror"
tmp_oldpath="$tmp_root/oldpath"
tmp_mixed="$tmp_root/mixed"

results=()
overall_failed=0

check() {
  local name="$1"
  shift
  echo "--- $name ---"
  if "$@"; then
    results+=("PASS: $name")
  else
    results+=("FAIL: $name")
    overall_failed=1
  fi
}

echo "=== generating mirror into $tmp_mirror ==="
go run ./internal/cmd/genforward \
  -src "$repo_root" \
  -out "$tmp_mirror" \
  -version v0.0.0-mirror-test \
  -mirror-module "$mirror_module" \
  -replace "$repo_root"

echo "=== verifying generated mirror ==="
check "mirror: GOOS=js GOARCH=wasm go build ./..." \
  bash -c "cd '$tmp_mirror' && GOOS=js GOARCH=wasm go build ./..."
check "mirror: GOOS=js GOARCH=wasm go vet ./..." \
  bash -c "cd '$tmp_mirror' && GOOS=js GOARCH=wasm go vet ./..."
# Only the mirror's root package is expected to build on the host platform:
# every other package (cloudflare/*, exp/hono) imports syscall/js
# unconditionally in the source module and has never built outside
# GOOS=js, so the mirror faithfully reproduces that limitation.
check "mirror: go build . (host, root package only)" \
  bash -c "cd '$tmp_mirror' && go build ."

echo "=== building mirror-tests/oldpath (mirror-only import) ==="
rm -rf "$tmp_oldpath"
cp -r "$script_dir/oldpath" "$tmp_oldpath"
sed -i.bak "s|OLDPATH_MIRROR_MODULE_PLACEHOLDER|$mirror_module|g" "$tmp_oldpath"/go.mod "$tmp_oldpath"/main.go
rm -f "$tmp_oldpath"/go.mod.bak "$tmp_oldpath"/main.go.bak
(cd "$tmp_oldpath" && go mod edit -replace "$mirror_module=$tmp_mirror")
# oldpath's own source never imports the source module directly, but the
# mirror's go.mod requires it at an unpublished test version; replace
# directives are not transitive, so the outer (main) module needs its own
# replace to resolve that requirement locally too.
(cd "$tmp_oldpath" && go mod edit -replace "$src_module=$repo_root")
(cd "$tmp_oldpath" && go mod tidy)
check "oldpath: GOOS=js GOARCH=wasm go build ./..." \
  bash -c "cd '$tmp_oldpath' && GOOS=js GOARCH=wasm go build ./..."

echo "=== building mirror-tests/mixed (mirror + source import) ==="
rm -rf "$tmp_mixed"
cp -r "$script_dir/mixed" "$tmp_mixed"
sed -i.bak \
  -e "s|MIXED_MIRROR_MODULE_PLACEHOLDER|$mirror_module|g" \
  -e "s|MIXED_SOURCE_MODULE_PLACEHOLDER|$src_module|g" \
  "$tmp_mixed"/go.mod "$tmp_mixed"/main.go
rm -f "$tmp_mixed"/go.mod.bak "$tmp_mixed"/main.go.bak
(cd "$tmp_mixed" && go mod edit -replace "$mirror_module=$tmp_mirror")
(cd "$tmp_mixed" && go mod edit -replace "$src_module=$repo_root")
(cd "$tmp_mixed" && go mod tidy)
check "mixed: GOOS=js GOARCH=wasm go build ./..." \
  bash -c "cd '$tmp_mixed' && GOOS=js GOARCH=wasm go build ./..."

echo
echo "================ mirror-tests summary ================"
for r in "${results[@]}"; do
  echo "$r"
done
echo "========================================================"

if [ "$overall_failed" -ne 0 ]; then
  echo "RESULT: FAIL"
  exit 1
fi
echo "RESULT: PASS"
