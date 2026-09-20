#!/usr/bin/env bash
# Shared by the macOS entry points; compatible with the system Bash 3.2.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME="${EPISTEME_RUNTIME_DIR:-$ROOT/.local}"
case "$RUNTIME" in /*) ;; *) echo 'EPISTEME_RUNTIME_DIR must be an absolute path.' >&2; exit 1 ;; esac
[[ "$RUNTIME" != / ]] || exit 1
export ROOT RUNTIME
set -a
source "$ROOT/scripts/versions.env"
set +a
export PATH="$RUNTIME/tools/go/bin:$RUNTIME/tools/node/bin:/opt/homebrew/opt/postgresql@18/bin:/opt/homebrew/opt/rustup/bin:/opt/homebrew/bin:$PATH"
export PYTHON_BIN="${PYTHON_BIN:-/opt/homebrew/bin/python3}"
[[ -x "$PYTHON_BIN" ]] || PYTHON_BIN="$(command -v python3 || true)"

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
platform_check() {
  [[ "$(uname -s)/$(uname -m)" == Darwin/arm64 ]] || die 'These scripts support native Apple Silicon macOS. For Linux/Intel use deploy/docker-compose.yml; do not run under Rosetta.'
  [[ "$(sw_vers -productVersion | cut -d. -f1)" -ge 15 ]] || die 'macOS 15 or newer is required.'
  [[ "$(id -u)" != 0 ]] || die 'Run as your normal user, not with sudo. Homebrew may request its own administrator access.'
}
