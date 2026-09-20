#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/lib/local.sh"
case "${1:-}" in
  -h|--help) echo 'Usage: ./start.sh [start|run|status|stop|restart|check] [postgres qdrant embedding reranker metadata api web]'; exit 0 ;;
esac
platform_check
[[ -x "$PYTHON_BIN" ]] || die 'Python is missing. Run ./INSTALL.sh first.'
exec "$PYTHON_BIN" "$ROOT/scripts/services.py" "${@:-start}"
