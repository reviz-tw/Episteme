"""Serialize install/start/stop without stale lock directories after interruption."""
import fcntl
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import signal

with (Path(os.environ["RUNTIME"]) / "stack.lock").open("a") as lock:
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        sys.exit("Another install/start/stop operation is in progress.")
    spec = importlib.util.spec_from_file_location("services", Path(__file__).with_name("services.py"))
    services = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(services)
    active = [name for name in services.SERVICES if services.running(name)]
    if active:
        sys.exit("Stop the managed services before installing/rebuilding: ./start.sh stop (running: " + ", ".join(active) + ")")
    env = dict(os.environ, EPISTEME_INSTALL_LOCKED="1")
    child = subprocess.Popen(["/bin/bash", *sys.argv[1:]], env=env)
    def terminate(*_):
        child.terminate()
        child.wait()
        sys.exit(143)
    signal.signal(signal.SIGTERM, terminate)
    sys.exit(child.wait())
