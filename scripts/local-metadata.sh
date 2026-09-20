#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "${1:-status}" == pull ]]; then
  exec "$root/INSTALL.sh" --models-only
fi
exec "$root/start.sh" "${1:-status}" metadata
