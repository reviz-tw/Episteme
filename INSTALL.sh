#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/lib/local.sh"
case "${1:-}" in
  -h|--help)
    echo 'Usage: ./INSTALL.sh [--models-only|--check]'
    echo 'Installs the pinned native Apple Silicon stack, models, and production build.'
    echo 'Safe to rerun. Does not overwrite local configuration, databases, or model revisions.'
    exit 0 ;;
  --check) exec "$ROOT/start.sh" check ;;
  ''|--models-only) ;;
  *) die "Unknown option: $1" ;;
esac
platform_check
if [[ "${1:-}" != --models-only ]] && ! xcode-select -p >/dev/null 2>&1; then
  xcode-select --install || true
  die 'Complete the Apple developer tools installer, install Xcode with Metal support, then rerun ./INSTALL.sh.'
fi
mkdir -p "$RUNTIME" "$RUNTIME/downloads" "$RUNTIME/tools" "$RUNTIME/bin" "$RUNTIME/logs"
# One installer at a time. services.py also holds this advisory lock during mutations.
if [[ "${EPISTEME_INSTALL_LOCKED:-}" != 1 && -x "$PYTHON_BIN" ]]; then
  exec "$PYTHON_BIN" "$ROOT/scripts/install-lock.py" "$0" "$@"
fi
trap 'printf "Installation interrupted or failed at line %s. Fix the error and rerun ./INSTALL.sh; verified downloads are reused.\n" "$LINENO" >&2' ERR

if [[ "${1:-}" == --models-only ]]; then
  [[ -x "$PYTHON_BIN" ]] || die 'Run the full installer first.'
  exec "$PYTHON_BIN" "$ROOT/scripts/download-models.py"
fi

if ! xcrun -f metal >/dev/null 2>&1; then
  xcodebuild -downloadComponent MetalToolchain || die 'Install/select full Xcode and accept its license, then rerun. Apple requires this system setup before Metal can be built.'
  xcrun -f metal >/dev/null || die 'Metal compiler is still unavailable.'
fi

if [[ ! -x /opt/homebrew/bin/brew ]]; then
  curl --fail --location --retry 3 https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o "$RUNTIME/downloads/homebrew-install.sh"
  /bin/bash "$RUNTIME/downloads/homebrew-install.sh"
fi
export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_CURL_RETRIES=3
brew=/opt/homebrew/bin/brew
for formula in postgresql@18 poppler cmake pkgconf protobuf openssl@3 rustup; do
  printf 'Checking Homebrew dependency: %s\n' "$formula"
  if ! "$brew" list --versions "$formula" >/dev/null 2>&1; then
    for attempt in 1 2 3; do
      if "$brew" install "$formula"; then break; fi
      [[ "$attempt" -lt 3 ]] || die "Homebrew could not install $formula; rerun to reuse downloaded bottles."
      sleep 2
    done
  fi
done
[[ -x "$PYTHON_BIN" ]] || die 'Python 3 from Xcode/Homebrew is required.'
[[ "$(postgres --version)" == *" $POSTGRES_MAJOR."* ]] || die "Expected PostgreSQL $POSTGRES_MAJOR. Refusing a major-version switch."
command -v pdftotext >/dev/null || die 'Poppler installation did not provide pdftotext.'

download() {
  local url="$1" hash="$2" out="$3"
  if [[ -f "$out" ]] && [[ "$(shasum -a 256 "$out" | cut -d' ' -f1)" == "$hash" ]]; then return; fi
  printf 'Downloading %s\n' "${url##*/}"
  curl --fail --location --retry 3 --connect-timeout 30 "$url" -o "$out.part"
  [[ "$(shasum -a 256 "$out.part" | cut -d' ' -f1)" == "$hash" ]] || die "SHA-256 mismatch: $out.part"
  mv "$out.part" "$out"
}

if [[ ! -x "$RUNTIME/tools/go/bin/go" ]] || [[ "$("$RUNTIME/tools/go/bin/go" version)" != "go version go$GO_VERSION darwin/arm64" ]]; then
  archive="$RUNTIME/downloads/go$GO_VERSION.darwin-arm64.tar.gz"
  download "https://go.dev/dl/go$GO_VERSION.darwin-arm64.tar.gz" "$GO_SHA256" "$archive"
  stage="$(mktemp -d "$RUNTIME/tools/go-stage.XXXXXX")"
  tar -xzf "$archive" -C "$stage"
  rm -rf "$RUNTIME/tools/go"
  mv "$stage/go" "$RUNTIME/tools/go"; rmdir "$stage"
fi
if [[ ! -x "$RUNTIME/tools/node/bin/node" ]] || [[ "$("$RUNTIME/tools/node/bin/node" --version)" != "v$NODE_VERSION" ]]; then
  archive="$RUNTIME/downloads/node-v$NODE_VERSION-darwin-arm64.tar.gz"
  download "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-darwin-arm64.tar.gz" "$NODE_SHA256" "$archive"
  stage="$(mktemp -d "$RUNTIME/tools/node-stage.XXXXXX")"
  tar -xzf "$archive" -C "$stage" --strip-components=1
  rm -rf "$RUNTIME/tools/node"
  mv "$stage" "$RUNTIME/tools/node"
fi
[[ "$(go version)" == "go version go$GO_VERSION darwin/arm64" ]] || die 'Go version mismatch.'
[[ "$(node --version)" == "v$NODE_VERSION" ]] || die 'Node version mismatch.'
export GOTOOLCHAIN=local NEXT_TELEMETRY_DISABLED=1

if [[ ! -x "$RUNTIME/bin/qdrant" ]] || [[ "$("$RUNTIME/bin/qdrant" --version)" != "qdrant $QDRANT_VERSION" ]]; then
  archive="$RUNTIME/downloads/qdrant-$QDRANT_VERSION.tar.gz"
  download "https://github.com/qdrant/qdrant/releases/download/v$QDRANT_VERSION/qdrant-aarch64-apple-darwin.tar.gz" "$QDRANT_SHA256" "$archive"
  stage="$(mktemp -d "$RUNTIME/tools/qdrant-stage.XXXXXX")"
  tar -xzf "$archive" -C "$stage"
  mv "$stage/qdrant" "$RUNTIME/bin/qdrant"; rmdir "$stage"
fi

tei_ok=false
if [[ -x "$RUNTIME/bin/text-embeddings-router" ]] && [[ "$("$RUNTIME/bin/text-embeddings-router" --version)" == "text-embeddings-router $TEI_VERSION" ]]; then
  if otool -L "$RUNTIME/bin/text-embeddings-router" | grep -q 'Metal.framework'; then tei_ok=true; fi
fi
if [[ "$tei_ok" != true ]]; then
  source_dir="$RUNTIME/build/tei-$TEI_COMMIT"
  mkdir -p "$RUNTIME/build"
  if [[ ! -d "$source_dir/.git" ]]; then
    git clone --depth 1 --branch "v$TEI_VERSION" https://github.com/huggingface/text-embeddings-inference.git "$source_dir"
  fi
  [[ "$(git -C "$source_dir" rev-parse HEAD)" == "$TEI_COMMIT" ]] || die 'TEI source commit mismatch.'
  rustup toolchain install "$RUST_VERSION" --profile minimal
  # TEI 1.9's original metrics 0.23.0 fails with E0521 on this compiler.
  if git -C "$source_dir" apply --check "$ROOT/scripts/tei-metrics.patch" 2>/dev/null; then
    git -C "$source_dir" apply "$ROOT/scripts/tei-metrics.patch"
  else
    git -C "$source_dir" apply --reverse --check "$ROOT/scripts/tei-metrics.patch" || die 'Unexpected TEI Cargo.lock; refusing to resolve different dependencies.'
  fi
  CARGO_TARGET_DIR="$RUNTIME/build/tei-target" rustup run "$RUST_VERSION" cargo install --locked \
    --path "$source_dir/router" --features metal --root "$RUNTIME" -j "${EPISTEME_BUILD_JOBS:-4}"
fi

ollama_dir="$RUNTIME/tools/ollama-$OLLAMA_VERSION"
if [[ ! -x "$ollama_dir/Ollama.app/Contents/Resources/ollama" ]]; then
  archive="$RUNTIME/downloads/Ollama-$OLLAMA_VERSION.zip"
  download "https://github.com/ollama/ollama/releases/download/v$OLLAMA_VERSION/Ollama-darwin.zip" "$OLLAMA_SHA256" "$archive"
  stage="$(mktemp -d "$RUNTIME/tools/ollama-stage.XXXXXX")"
  ditto -xk "$archive" "$stage"
  [[ -x "$stage/Ollama.app/Contents/Resources/ollama" ]] || die 'Invalid Ollama archive.'
  mv "$stage" "$ollama_dir"
fi
"$PYTHON_BIN" "$ROOT/scripts/download-models.py"

printf '\nBuilding Episteme with Go %s / Node %s (lockfiles enforced)…\n' "$GO_VERSION" "$NODE_VERSION"
cd "$ROOT"
go mod download
go mod verify
mkdir -p bin
for app in server mcp-stdio export; do
  go build -mod=readonly -trimpath -o "bin/$app.new" "./cmd/$app"
  mv "bin/$app.new" "bin/$app"
done
(cd web && npm ci --no-audit --no-fund && API_INTERNAL_URL=http://127.0.0.1:8080 npm run build)
"$PYTHON_BIN" "$ROOT/scripts/services.py" configure
"$brew" list --versions > "$RUNTIME/installed-brew-versions.txt"
cp "$ROOT/scripts/versions.env" "$RUNTIME/installed-versions.env"
"$ROOT/start.sh" check
printf '\nInstallation complete. Start all services: ./start.sh\nConfiguration: %s/config.json\n' "$RUNTIME"
