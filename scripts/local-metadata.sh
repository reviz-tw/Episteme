#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime="$root/.local"
binary="${OLLAMA_BIN:-/Applications/Ollama.app/Contents/Resources/ollama}"
if [[ ! -x "$binary" ]]; then binary="$(command -v ollama || true)"; fi
[[ -x "$binary" ]] || { echo 'Install Ollama first: https://ollama.com/download'; exit 1; }
mkdir -p "$runtime/pids" "$runtime/logs" "$runtime/ollama/models"
export OLLAMA_HOST=127.0.0.1:11434 OLLAMA_MODELS="$runtime/ollama/models" OLLAMA_NO_CLOUD=1 OLLAMA_NUM_PARALLEL=1
model="${METADATA_MODEL:-gemma2:2b}"
pidfile="$runtime/pids/metadata.pid"
running() {
 local pid command
 [[ -f "$pidfile" ]] || return 1
 pid="$(cat "$pidfile")"; [[ "$pid" =~ ^[0-9]+$ ]] || return 1
 command="$(ps -p "$pid" -o command= 2>/dev/null)" || return 1
 [[ "$command" == "$binary serve"* ]]
}
case "${1:-status}" in
 start|run)
  if running; then echo 'Metadata model service is already running.'; exit 0; fi
  if lsof -nP -iTCP:11434 -sTCP:LISTEN >/dev/null 2>&1; then
   echo 'Port 11434 is already occupied; leaving that service unchanged.' >&2; exit 1
  fi
  if [[ "$1" == run ]]; then
   printf '%s\n' "$$" > "$pidfile"
   exec "$binary" serve
  else
   nohup "$binary" serve >"$runtime/logs/metadata.log" 2>&1 </dev/null &
   printf '%s\n' "$!" > "$pidfile"
   echo 'Starting metadata model service at 127.0.0.1:11434.'
  fi
  ;;
 pull) "$binary" pull "$model" ;;
 status)
  curl --fail --silent --show-error --max-time 3 http://127.0.0.1:11434/api/tags |
   python3 -c 'import json,sys; name=sys.argv[1]; ready=any(m["name"]==name for m in json.load(sys.stdin)["models"]); print(name+": "+("ready" if ready else "not downloaded; run scripts/local-metadata.sh pull")); sys.exit(0 if ready else 1)' "$model"
  ;;
 stop)
  if running; then kill "$(cat "$pidfile")"; echo 'Stopping metadata model service.'; fi
  rm -f "$pidfile"
  ;;
 *) echo "Usage: $0 start|run|pull|status|stop" >&2; exit 2 ;;
esac
