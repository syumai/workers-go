#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec go run -C "$ROOT/internal/cmd/genforward" . -src "$ROOT" "$@"
