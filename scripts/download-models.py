"""Pinned, resumable model downloads using only the Python standard library + curl.

Use the existing Hugging Face / Ollama cache layouts so old downloads are reused.
No model server or Hugging Face token is needed to install these public artifacts.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
RUNTIME = Path(os.environ.get("RUNTIME", ROOT / ".local"))
LOCK = json.loads((ROOT / "scripts/models.lock.json").read_text())


def digest(path, algorithm, size):
    hasher = hashlib.sha256() if algorithm == "sha256" else hashlib.sha1()
    if algorithm == "git-sha1":
        hasher.update(f"blob {size}\0".encode())
    with path.open("rb") as source:
        for block in iter(lambda: source.read(8 * 1024 * 1024), b""):
            hasher.update(block)
    return hasher.hexdigest()


def valid(path, spec, full=True):
    return (path.is_file() and path.stat().st_size == spec["size"]
            and (not full or digest(path, spec["algorithm"], spec["size"]) == spec["hash"]))


def download(url, path, spec):
    if valid(path, spec):
        print(f"Verified cached model file: {path.name}", flush=True)
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    partial = path.with_name(path.name + ".partial")
    if valid(partial, spec):
        partial.replace(path)
        return
    print(f"Downloading {path.name} ({spec['size'] / 1e6:.1f} MB)", flush=True)
    command = ["curl", "--fail", "--location", "--retry", "3", "--connect-timeout", "30"]
    result = subprocess.run([*command, "--continue-at", "-", "--output", str(partial), url])
    if result.returncode == 33:  # A server without range support requires a fresh transfer.
        subprocess.run([*command, "--output", str(partial), url], check=True)
    elif result.returncode:
        raise RuntimeError(f"Download failed ({result.returncode}); rerun INSTALL.sh to resume.")
    if not valid(partial, spec):
        partial.unlink(missing_ok=True)
        raise RuntimeError(f"Model checksum mismatch: {path.name}; discarded the incomplete file.")
    partial.replace(path)


def hf_paths(model, spec):
    cache = RUNTIME / "models" / ("models--" + model["repo"].replace("/", "--"))
    return cache / "blobs" / spec["hash"], cache / "snapshots" / model["revision"] / spec["path"]


def ollama_parts():
    manifest = LOCK["ollama"]["manifest"]
    for layer in [manifest["config"], *manifest["layers"]]:
        sha = layer["digest"].removeprefix("sha256:")
        yield RUNTIME / "ollama/models/blobs" / ("sha256-" + sha), {
            "hash": sha, "size": layer["size"], "algorithm": "sha256"}


def check(full=False):
    for model in LOCK["huggingface"]:
        for spec in model["files"]:
            _, path = hf_paths(model, spec)
            if not valid(path, spec, full):
                raise RuntimeError(f"Missing/incomplete model file: {path}. Run ./INSTALL.sh --models-only.")
        link = RUNTIME / "model-links" / model["repo"]
        # The exact target is checked, not just existence of a similarly named model.
        expected = RUNTIME / "models" / ("models--" + model["repo"].replace("/", "--")) / "snapshots" / model["revision"]
        if link.resolve() != expected.resolve():
            raise RuntimeError(f"Wrong pinned model link: {link}")
    manifest = RUNTIME / "ollama/models/manifests/registry.ollama.ai/library/gemma2/2b"
    if not manifest.is_file() or hashlib.sha256(manifest.read_bytes()).hexdigest() != LOCK["ollama"]["digest"]:
        raise RuntimeError("Gemma manifest differs from the pinned version. Run INSTALL.sh --models-only.")
    for path, spec in ollama_parts():
        if not valid(path, spec, full):
            raise RuntimeError(f"Missing/incomplete Gemma blob: {path.name}")


def install():
    for model in LOCK["huggingface"]:
        for spec in model["files"]:
            blob, target = hf_paths(model, spec)
            download(f"https://huggingface.co/{model['repo']}/resolve/{model['revision']}/{spec['path']}", blob, spec)
            target.parent.mkdir(parents=True, exist_ok=True)
            if not target.is_symlink() or target.resolve() != blob.resolve():
                target.unlink(missing_ok=True)
                target.symlink_to(os.path.relpath(blob, target.parent))
        link = RUNTIME / "model-links" / model["repo"]
        link.parent.mkdir(parents=True, exist_ok=True)
        snapshot = RUNTIME / "models" / ("models--" + model["repo"].replace("/", "--")) / "snapshots" / model["revision"]
        if link.is_symlink():
            link.unlink()
        elif link.exists():
            raise RuntimeError(f"Refusing to replace a non-symlink model directory: {link}")
        link.symlink_to(os.path.relpath(snapshot, link.parent))
    for path, spec in ollama_parts():
        download(f"https://registry.ollama.ai/v2/library/gemma2/blobs/sha256:{spec['hash']}", path, spec)
    # The compact manifest bytes are themselves pinned; write only after every blob verifies.
    content = json.dumps(LOCK["ollama"]["manifest"], separators=(",", ":")).encode()
    if hashlib.sha256(content).hexdigest() != LOCK["ollama"]["digest"]:
        raise RuntimeError("Invalid checked-in Gemma manifest")
    manifest = RUNTIME / "ollama/models/manifests/registry.ollama.ai/library/gemma2/2b"
    manifest.parent.mkdir(parents=True, exist_ok=True)
    temporary = manifest.with_suffix(".tmp")
    temporary.write_bytes(content)
    temporary.replace(manifest)
    check()
    print("All three pinned models are installed and verified.")


if __name__ == "__main__":
    try:
        if "--check" in sys.argv:
            check(full=True)
            print("All model checksums verified.")
        else:
            install()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
