#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

(cd "$ROOT/internal/cmd/genforward" && go build -o "$tmp/genforward" .)

"$tmp/genforward" -src "$ROOT" -static "$ROOT/internal/cmd/genforward/_static" "$@"
