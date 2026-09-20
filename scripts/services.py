"""Native local stack lifecycle. No third-party Python dependencies."""
import contextlib
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
RUNTIME = Path(os.environ.get("RUNTIME", ROOT / ".local"))
SERVICES = ("postgres", "qdrant", "embedding", "reranker", "metadata", "api", "web")
VERSIONS = dict(line.split("=", 1) for line in (ROOT / "scripts/versions.env").read_text().splitlines() if line and not line.startswith("#"))
PG_BIN = Path("/opt/homebrew/opt/postgresql@18/bin")
NODE = RUNTIME / "tools/node/bin/node"
OLLAMA = RUNTIME / f"tools/ollama-{VERSIONS['OLLAMA_VERSION']}/Ollama.app/Contents/Resources/ollama"
CONFIG = RUNTIME / "config.json"
CHILDREN = {}
spec = importlib.util.spec_from_file_location("models", ROOT / "scripts/download-models.py")
models = importlib.util.module_from_spec(spec)
spec.loader.exec_module(models)


def say(message):
    print(message, flush=True)


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(".tmp")
    with open(temporary, "w", opener=lambda p, flags: os.open(p, flags, 0o600)) as out:
        json.dump(value, out, indent=2)
        out.write("\n")
    os.chmod(temporary, 0o600)
    temporary.replace(path)


def configure():
    if CONFIG.exists():
        load_config()
        say(f"Keeping existing configuration: {CONFIG}")
        return
    atomic_json(CONFIG, {
        "postgres_port": int(os.environ.get("EPISTEME_PG_PORT", "55439")),
        "postgres_data": str(RUNTIME / "postgres"),
        "postgres_user": "episteme", "postgres_database": "episteme",
        "postgres_password": secrets.token_hex(24),
        "app_origin": "http://localhost:3000", "mcp_token": "",
    })
    say(f"Created private local configuration: {CONFIG}")


def load_config():
    if not CONFIG.is_file():
        raise RuntimeError("Installation/configuration missing. Run ./INSTALL.sh first.")
    config = json.loads(CONFIG.read_text())
    if not isinstance(config["postgres_port"], int) or not 1024 <= config["postgres_port"] <= 65535:
        raise RuntimeError("postgres_port must be an integer from 1024 to 65535.")
    if config["postgres_port"] in {3000, 8080, 8081, 8082, 6333, 6334, 11434}:
        raise RuntimeError("PostgreSQL port conflicts with another Episteme service.")
    for name in ("postgres_user", "postgres_database"):
        if not config[name] or not all(c.isascii() and (c.isalnum() or c == "_") for c in config[name]):
            raise RuntimeError(f"Invalid {name}: use ASCII letters, digits, underscores.")
    if not Path(config["postgres_data"]).is_absolute() or config["postgres_data"] == "/":
        raise RuntimeError("postgres_data must be an absolute data-directory path.")
    if config["app_origin"] not in ("http://localhost:3000", "http://127.0.0.1:3000"):
        raise RuntimeError("The native stack binds to loopback; app_origin must use localhost or 127.0.0.1 on port 3000.")
    return config


def capture(command):
    return subprocess.check_output([str(v) for v in command], text=True, stderr=subprocess.STDOUT).strip()


def installation_check(config):
    for binary in (PG_BIN / "postgres", PG_BIN / "initdb", PG_BIN / "psql", NODE, OLLAMA,
                   RUNTIME / "tools/go/bin/go", RUNTIME / "bin/qdrant", RUNTIME / "bin/text-embeddings-router",
                   ROOT / "bin/server", ROOT / "bin/mcp-stdio", ROOT / "bin/export"):
        if not os.access(binary, os.X_OK):
            raise RuntimeError(f"Missing executable: {binary}. Run ./INSTALL.sh.")
    checks = [([PG_BIN / "postgres", "--version"], f" {VERSIONS['POSTGRES_MAJOR']}."),
              ([NODE, "--version"], f"v{VERSIONS['NODE_VERSION']}"),
              ([RUNTIME / "tools/go/bin/go", "version"], f"go{VERSIONS['GO_VERSION']} "),
              ([RUNTIME / "bin/qdrant", "--version"], f"qdrant {VERSIONS['QDRANT_VERSION']}"),
              ([RUNTIME / "bin/text-embeddings-router", "--version"], f"text-embeddings-router {VERSIONS['TEI_VERSION']}")]
    for command, expected in checks:
        if expected not in capture(command):
            raise RuntimeError(f"Version mismatch: {command[0]}. Run ./INSTALL.sh.")
    pg_version = Path(config["postgres_data"]) / "PG_VERSION"
    if pg_version.exists() and pg_version.read_text().strip() != VERSIONS["POSTGRES_MAJOR"]:
        raise RuntimeError("PostgreSQL data uses another major version. Back up and migrate explicitly; it will not be reinitialized.")
    if not (ROOT / "web/.next/standalone/server.js").is_file():
        raise RuntimeError("Next.js production build is missing. Run ./INSTALL.sh.")
    routes = json.loads((ROOT / "web/.next/routes-manifest.json").read_text())
    rewrites = routes["rewrites"]
    if isinstance(rewrites, dict):
        rewrites = [item for group in rewrites.values() for item in group]
    if not any(rule.get("destination") == "http://127.0.0.1:8080/api/:path*" for rule in rewrites):
        raise RuntimeError("Next.js was built for another API URL. Run ./INSTALL.sh to rebuild the local proxy.")
    capture(["pdftotext", "-v"])
    revisions = {model["repo"]: model["revision"] for model in models.LOCK["huggingface"]}
    if (revisions.get("BAAI/bge-m3") != VERSIONS["EMBEDDING_REVISION"]
            or revisions.get("BAAI/bge-reranker-v2-m3") != VERSIONS["RERANKER_REVISION"]
            or models.LOCK["ollama"]["digest"] != VERSIONS["GEMMA_DIGEST"]):
        raise RuntimeError("versions.env and models.lock.json disagree; update and verify them together.")
    models.check()


def process_stamp(pid):
    if not isinstance(pid, int) or pid < 2:
        return None
    try:
        # Start time prevents a stale PID file from targeting a reused PID.
        value = capture(["ps", "-p", pid, "-o", "uid=,lstart=,stat="])
        if not value or value.split()[0] != str(os.getuid()) or "Z" in value.split()[-1]:
            return None
        return " ".join(value.split()[:-1])
    except subprocess.CalledProcessError:
        return None


def record_path(name):
    return RUNTIME / "pids" / (name + ".json")


def running(name):
    try:
        record = json.loads(record_path(name).read_text())
        return record if process_stamp(record["pid"]) == record["stamp"] and record["stamp"] else None
    except (OSError, ValueError, KeyError):
        return None


def ports(name, config):
    return {"postgres": [config["postgres_port"]], "qdrant": [6333, 6334], "embedding": [8081],
            "reranker": [8082], "metadata": [11434], "api": [8080], "web": [3000]}[name]


def free_ports(name, config):
    for port in ports(name, config):
        # Check both families: ::1 or an IPv6 wildcard can also occupy a port.
        for family, host in ((socket.AF_INET, "127.0.0.1"), (socket.AF_INET6, "::1")):
            with socket.socket(family, socket.SOCK_STREAM) as probe:
                # Match server bind semantics: a recently closed connection in TIME_WAIT
                # is not a foreign listener and must not block an immediate restart.
                probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                try:
                    probe.bind((host, port))
                except OSError as error:
                    raise RuntimeError(f"Port {port} is occupied; refusing to start {name}. Stop its existing owner first.") from error


def request(path, port, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", data=data,
                                 headers={"Content-Type": "application/json"})
    # Local traffic must not be sent through a configured corporate HTTP proxy.
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=3) as response:
        return response.read()


def healthy(name, config):
    try:
        if name == "postgres":
            result = subprocess.run([str(PG_BIN / "pg_isready"), "-h", "127.0.0.1", "-p", str(config["postgres_port"])],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=4)
            return result.returncode == 0
        path = {"qdrant": "/healthz", "embedding": "/health", "reranker": "/health", "metadata": "/api/tags",
                "api": "/healthz", "web": "/api/v1/auth/me"}[name]
        body = request(path, ports(name, config)[0])
        if name == "metadata":
            return any(m["name"] == "gemma2:2b" and m["digest"] == VERSIONS["GEMMA_DIGEST"] for m in json.loads(body)["models"])
        return True
    except urllib.error.HTTPError as error:
        # /auth/me is an authenticated endpoint; 401 proves the web proxy reaches API.
        return name == "web" and error.code == 401
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        return False


def pg_environment(config):
    return dict(os.environ, PGHOST="127.0.0.1", PGPORT=str(config["postgres_port"]),
                PGUSER=config["postgres_user"], PGPASSWORD=config["postgres_password"], PGCONNECT_TIMEOUT="3")


def ensure_database(config):
    env = pg_environment(config)
    base = [str(PG_BIN / "psql"), "-X", "-v", "ON_ERROR_STOP=1", "-d", "postgres", "-tAc"]
    name = config["postgres_database"]  # Strict ASCII identifier validation in load_config.
    exists = subprocess.check_output([*base, f"SELECT 1 FROM pg_database WHERE datname='{name}'"], env=env, text=True).strip()
    if exists != "1":
        subprocess.run([str(PG_BIN / "createdb"), name], env=env, check=True)


def initialize_postgres(config):
    data = Path(config["postgres_data"])
    if (data / "PG_VERSION").exists():
        return
    if data.exists() and any(data.iterdir()):
        raise RuntimeError(f"Nonempty PostgreSQL directory without PG_VERSION: {data}. Refusing to overwrite it.")
    password_file = RUNTIME / "initdb-password.tmp"
    try:
        with open(password_file, "w", opener=lambda p, flags: os.open(p, flags, 0o600)) as out:
            out.write(config["postgres_password"] + "\n")
        subprocess.run([str(PG_BIN / "initdb"), "-D", str(data), "--username", config["postgres_user"],
                        "--encoding=UTF8", "--locale=C", "--auth=scram-sha-256", "--pwfile", str(password_file)], check=True)
    finally:
        password_file.unlink(missing_ok=True)


def command_for(name, config):
    env = dict(os.environ)
    for key in ("HF_TOKEN", "HF_API_TOKEN", "HUGGING_FACE_HUB_TOKEN", "MODEL_ID", "REVISION"):
        env.pop(key, None)
    cwd = RUNTIME
    if name == "postgres":
        initialize_postgres(config)
        # Disable Unix sockets to avoid platform path-length limits and shared /tmp sockets.
        command = [PG_BIN / "postgres", "-D", config["postgres_data"], "-h", "127.0.0.1", "-p", config["postgres_port"], "-k", ""]
    elif name == "qdrant":
        cwd = RUNTIME / "qdrant"
        cwd.mkdir(exist_ok=True)
        env.update(QDRANT__SERVICE__HOST="127.0.0.1", QDRANT__SERVICE__HTTP_PORT="6333", QDRANT__SERVICE__GRPC_PORT="6334",
                   QDRANT__TELEMETRY_DISABLED="true", QDRANT__STORAGE__STORAGE_PATH=str(cwd / "storage"),
                   QDRANT__STORAGE__SNAPSHOTS_PATH=str(cwd / "snapshots"))
        command = [RUNTIME / "bin/qdrant"]
    elif name in ("embedding", "reranker"):
        repo = "BAAI/bge-m3" if name == "embedding" else "BAAI/bge-reranker-v2-m3"
        revision = VERSIONS["EMBEDDING_REVISION" if name == "embedding" else "RERANKER_REVISION"]
        # A relative local path keeps /info.model_id == BAAI/... while avoiding Hub requests entirely.
        cwd = RUNTIME / "model-links"
        env.update(HF_HOME=str(RUNTIME / "hf"), HF_HUB_OFFLINE="1", AUTO_TRUNCATE="false")
        command = [RUNTIME / "bin/text-embeddings-router", "--model-id", repo, "--revision", revision,
                   "--hostname", "127.0.0.1", "--port", ports(name, config)[0], "--max-concurrent-requests", "64",
                   "--max-batch-requests", "4", "--max-batch-tokens", "8192", "--tokenization-workers", "2"]
    elif name == "metadata":
        env.update(OLLAMA_HOST="127.0.0.1:11434", OLLAMA_MODELS=str(RUNTIME / "ollama/models"),
                   OLLAMA_NO_CLOUD="1", OLLAMA_NUM_PARALLEL="1")
        command = [OLLAMA, "serve"]
    elif name == "api":
        user = urllib.parse.quote(config["postgres_user"], safe="")
        password = urllib.parse.quote(config["postgres_password"], safe="")
        env.update(DATABASE_URL=f"postgres://{user}:{password}@127.0.0.1:{config['postgres_port']}/{config['postgres_database']}?sslmode=disable",
                   LISTEN_ADDR="127.0.0.1:8080", APP_ORIGIN=config["app_origin"], COOKIE_SECURE="false",
                   TEI_EMBED_URL="http://127.0.0.1:8081", TEI_RERANK_URL="http://127.0.0.1:8082",
                   EMBEDDING_MODEL="BAAI/bge-m3", EMBEDDING_DIMENSION="1024", QDRANT_HOST="127.0.0.1",
                   QDRANT_GRPC_PORT="6334", QDRANT_TLS="false", QDRANT_API_KEY="", QDRANT_COLLECTION="knowledge_base",
                   METADATA_ENABLED="true", METADATA_OLLAMA_URL="http://127.0.0.1:11434", METADATA_MODEL="gemma2:2b",
                   MCP_TOKEN=config["mcp_token"])
        command = [ROOT / "bin/server"]
    else:
        cwd = ROOT / "web"
        env.update(NODE_ENV="production", HOSTNAME="127.0.0.1", PORT="3000", NEXT_TELEMETRY_DISABLED="1")
        command = [NODE, ROOT / "web/scripts/start.mjs"]
    return [str(value) for value in command], cwd, env


def stop_one(name):
    record = running(name)
    if not record:
        record_path(name).unlink(missing_ok=True)
        return
    say(f"Stopping {name} (PID {record['pid']})…")
    os.kill(record["pid"], signal.SIGINT if name == "postgres" else signal.SIGTERM)
    deadline = time.monotonic() + 30
    while running(name) and time.monotonic() < deadline:
        time.sleep(0.2)
    if running(name):
        raise RuntimeError(f"{name} did not stop within 30 seconds; leaving the PID record intact. Inspect its log before retrying.")
    if name in CHILDREN:
        CHILDREN.pop(name).wait(timeout=3)
    record_path(name).unlink(missing_ok=True)


def start_all(selected, config):
    installation_check(config)
    # Preflight every requested port before creating a DB or starting any services.
    for name in selected:
        if not running(name):
            free_ports(name, config)
    started = []
    try:
        for name in selected:
            if not running(name):
                command, cwd, env = command_for(name, config)
                with (RUNTIME / "logs" / f"{name}.log").open("ab") as log:
                    proc = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                            stdout=log, stderr=log, start_new_session=True)
                CHILDREN[name] = proc
                stamp = process_stamp(proc.pid)
                if stamp is None:
                    if proc.poll() is None:
                        proc.terminate()
                    proc.wait(timeout=5)
                    CHILDREN.pop(name, None)
                    raise RuntimeError(f"{name} exited immediately. Inspect .local/logs/{name}.log.")
                try:
                    atomic_json(record_path(name), {"pid": proc.pid, "stamp": stamp})
                except BaseException:
                    proc.terminate()
                    proc.wait(timeout=5)
                    CHILDREN.pop(name, None)
                    raise
                started.append(name)
                say(f"Starting {name} (PID {proc.pid})…")
            timeout = 300 if name in ("embedding", "reranker") else 60
            deadline = time.monotonic() + timeout
            while not healthy(name, config):
                if not running(name) or time.monotonic() >= deadline:
                    raise RuntimeError(f"{name} failed its health check. Inspect {RUNTIME}/logs/{name}.log.")
                time.sleep(1)
            if name == "postgres":
                ensure_database(config)
            if name in ("embedding", "reranker"):
                info = json.loads(request("/info", ports(name, config)[0]))
                expected_model = "BAAI/bge-m3" if name == "embedding" else "BAAI/bge-reranker-v2-m3"
                expected_sha = VERSIONS["EMBEDDING_REVISION" if name == "embedding" else "RERANKER_REVISION"]
                if info["model_id"] != expected_model or info["model_sha"] != expected_sha or info["version"] != VERSIONS["TEI_VERSION"]:
                    raise RuntimeError(f"{name} served an unexpected model/revision/version.")
            if name == "metadata" and json.loads(request("/api/version", 11434))["version"] != VERSIONS["OLLAMA_VERSION"]:
                raise RuntimeError("Unexpected Ollama server version.")
            say(f"{name}: ready")
    except BaseException:
        for name in reversed(started):
            try:
                stop_one(name)
            except (OSError, RuntimeError) as error:
                say(str(error))
        raise


def status(selected, config):
    failed = False
    for name in selected:
        record = running(name)
        ready = bool(record) and healthy(name, config)
        state = "ready" if ready else "unhealthy" if record else "not managed / stopped"
        say(f"{name}: {state} (127.0.0.1:{ports(name, config)[0]})")
        failed |= not ready
    return 1 if failed else 0


@contextlib.contextmanager
def operation_lock():
    with (RUNTIME / "stack.lock").open("a") as lock:
        if os.environ.get("EPISTEME_INSTALL_LOCKED") != "1":
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise RuntimeError("Another install/start/stop operation is in progress.") from error
        yield


def main():
    args = sys.argv[1:] or ["start"]
    action, names = args[0], args[1:]
    if action not in ("start", "run", "stop", "restart", "status", "check", "configure") or any(n not in SERVICES for n in names):
        raise RuntimeError("Usage: ./start.sh [start|run|status|stop|restart|check] [service …]")
    selected = [n for n in SERVICES if n in names or not names]
    RUNTIME.mkdir(parents=True, exist_ok=True)
    for directory in ("pids", "logs"):
        (RUNTIME / directory).mkdir(exist_ok=True)
    if action == "configure":
        with operation_lock():
            configure()
        return 0
    config = load_config()
    if action == "check":
        installation_check(config)
        say("Installed binaries, versions, PDF tool, production build and pinned model files: OK")
        return 0
    if action == "status":
        return status(selected, config)
    with operation_lock():
        if action in ("stop", "restart"):
            for name in reversed(selected):
                stop_one(name)
        if action in ("start", "run", "restart"):
            start_all(selected, config)
            if "web" in selected:
                say(f"Episteme is ready: {config['app_origin']}\nLogs: {RUNTIME}/logs")
    if action == "run":
        say("Foreground supervision active. Ctrl+C stops this selected stack.")
        try:
            while True:
                time.sleep(3)
                for name in selected:
                    if not running(name):
                        raise RuntimeError(f"{name} exited; stopping the selected stack. See its log.")
        finally:
            with operation_lock():
                for name in reversed(selected):
                    stop_one(name)
    return 0


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        sys.exit(f"ERROR: {error}")
