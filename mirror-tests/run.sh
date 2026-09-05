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
# script keeps working unchanged if the module path ever moves again.
src_module="$(awk '/^module[ \t]/{print $2; exit}' go.mod)"
if [ -z "$src_module" ]; then
  echo "could not read module path from $repo_root/go.mod" >&2
  exit 1
fi

# github.com/syumai/workers is the mirror module path used in production
# (see .github/workflows/release.yml). The source module is
# github.com/syumai/workers-go, so the two paths never collide.
mirror_module="github.com/syumai/workers"

echo "source module: $src_module"
echo "mirror module: $mirror_module"

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
"$repo_root/scripts/genforward.sh" \
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
check "mirror: static files copied (README.md, cmd/workers-assets-gen/main.go, LICENSE.md)" \
  bash -c "[ -f '$tmp_mirror/README.md' ] && [ -f '$tmp_mirror/cmd/workers-assets-gen/main.go' ] && [ -f '$tmp_mirror/LICENSE.md' ]"

echo "=== building mirror-tests/oldpath (mirror-only import) ==="
rm -rf "$tmp_oldpath"
cp -r "$script_dir/oldpath" "$tmp_oldpath"
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
