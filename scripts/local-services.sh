#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runtime="$root/.local"
mkdir -p "$runtime/pids" "$runtime/logs" "$runtime/models" "$runtime/hf" "$runtime/qdrant"

binary_for() {
  if [[ "$1" == qdrant ]]; then
    printf '%s/bin/qdrant' "$runtime"
  else
    printf '%s/bin/text-embeddings-router' "$runtime"
  fi
}

running() {
  local service="$1" pid command
  [[ -f "$runtime/pids/$service.pid" ]] || return 1
  pid="$(cat "$runtime/pids/$service.pid")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  command="$(ps -p "$pid" -o command= 2>/dev/null)" || return 1
  [[ "$command" == "$(binary_for "$service")"* ]]
}

start_service() {
  local service="$1" port="$2"
  shift 2
  if running "$service"; then
    printf '%s already running (PID %s)\n' "$service" "$(cat "$runtime/pids/$service.pid")"
    return
  fi
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    printf 'Port %s is already in use; refusing to start %s.\n' "$port" "$service" >&2
    return 1
  fi
  nohup "$@" >"$runtime/logs/$service.log" 2>&1 < /dev/null &
  printf '%s\n' "$!" > "$runtime/pids/$service.pid"
  printf 'Starting %s on 127.0.0.1:%s; log: %s/logs/%s.log\n' "$service" "$port" "$runtime" "$service"
}

status() {
  local failed=0 service port path
  for service in qdrant embedding reranker; do
    case "$service" in
      qdrant) port=6333; path=healthz ;;
      embedding) port=8081; path=health ;;
      reranker) port=8082; path=health ;;
    esac
    if curl --fail --silent --max-time 3 "http://127.0.0.1:$port/$path" >/dev/null; then
      printf '%s: ready (127.0.0.1:%s)\n' "$service" "$port"
    elif running "$service"; then
      printf '%s: starting; inspect %s/logs/%s.log\n' "$service" "$runtime" "$service"
      failed=1
    else
      printf '%s: stopped; inspect %s/logs/%s.log\n' "$service" "$runtime" "$service"
      failed=1
    fi
  done
  return "$failed"
}

case "${1:-status}" in
  start|run)
    for binary in qdrant text-embeddings-router; do
      [[ -x "$runtime/bin/$binary" ]] || { printf 'Missing %s/bin/%s; see docs/local-macos.md\n' "$runtime" "$binary" >&2; exit 1; }
    done
    cd "$runtime/qdrant"
    start_service qdrant 6333 env \
      QDRANT__SERVICE__HOST=127.0.0.1 QDRANT__SERVICE__HTTP_PORT=6333 QDRANT__SERVICE__GRPC_PORT=6334 \
      QDRANT__TELEMETRY_DISABLED=true \
      QDRANT__STORAGE__STORAGE_PATH="$runtime/qdrant/storage" \
      QDRANT__STORAGE__SNAPSHOTS_PATH="$runtime/qdrant/snapshots" \
      "$runtime/bin/qdrant"
    for service in embedding reranker; do
      if [[ "$service" == embedding ]]; then
        model=BAAI/bge-m3; port=8081
      else
        model=BAAI/bge-reranker-v2-m3; port=8082
      fi
      start_service "$service" "$port" env -u HF_TOKEN -u HF_API_TOKEN -u HUGGING_FACE_HUB_TOKEN \
        HF_HOME="$runtime/hf" AUTO_TRUNCATE=false \
        "$runtime/bin/text-embeddings-router" \
        --model-id "$model" --hostname 127.0.0.1 --port "$port" \
        --huggingface-hub-cache "$runtime/models" \
        --max-concurrent-requests 64 --max-batch-requests 4 --max-batch-tokens 8192 \
        --tokenization-workers 2
    done
    if [[ "${1:-}" == run ]]; then
      trap '"$root/scripts/local-services.sh" stop; exit' INT TERM
      wait
    fi
    ;;
  status) status ;;
  stop)
    for service in embedding reranker qdrant; do
      if running "$service"; then
        kill "$(cat "$runtime/pids/$service.pid")"
        printf 'Stopping %s\n' "$service"
      fi
      rm -f "$runtime/pids/$service.pid"
    done
    ;;
  *) printf 'Usage: %s start|run|status|stop\n' "$0" >&2; exit 2 ;;
esac
