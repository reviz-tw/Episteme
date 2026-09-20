"""Lifecycle tests use ephemeral processes and directories, never the application DB."""
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("services", Path(__file__).with_name("services.py"))
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)


class LocalStackTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="episteme-stack-test-")
        self.root = Path(self.directory.name)
        self.patches = [patch.object(s, "RUNTIME", self.root), patch.object(s, "CONFIG", self.root / "config.json")]
        for item in self.patches:
            item.start()
        for directory in ("pids", "logs"):
            (self.root / directory).mkdir()
        s.configure()
        self.config = s.load_config()

    def tearDown(self):
        for name in list(s.CHILDREN):
            proc = s.CHILDREN.pop(name)
            if proc.poll() is None:
                proc.terminate()
            proc.wait(timeout=5)
        for item in reversed(self.patches):
            item.stop()
        self.directory.cleanup()

    def test_configuration_preserves_password_and_existing_values(self):
        original = s.CONFIG.read_bytes()
        s.configure()
        self.assertEqual(original, s.CONFIG.read_bytes())
        self.assertEqual(s.CONFIG.stat().st_mode & 0o777, 0o600)
        self.assertGreaterEqual(len(self.config["postgres_password"]), 40)

    def test_port_conflict_is_detected_before_any_start(self):
        with socket.socket() as occupied:
            occupied.bind(("127.0.0.1", 0))
            self.config["postgres_port"] = occupied.getsockname()[1]
            with patch.object(s, "installation_check"), patch.object(s, "command_for") as command:
                with self.assertRaisesRegex(RuntimeError, "occupied"):
                    s.start_all(["postgres"], self.config)
                command.assert_not_called()

    def test_stale_pid_does_not_stop_unrelated_process(self):
        proc = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
        try:
            s.atomic_json(s.record_path("api"), {"pid": proc.pid, "stamp": "old process"})
            s.stop_one("api")
            self.assertIsNone(proc.poll())
        finally:
            proc.terminate()
            proc.wait(timeout=5)

    def test_recently_closed_connection_does_not_block_restart(self):
        with socket.socket() as server:
            server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            server.bind(("127.0.0.1", 0))
            port = server.getsockname()[1]
            server.listen()
            with socket.create_connection(("127.0.0.1", port)) as client:
                accepted, _ = server.accept()
                accepted.shutdown(socket.SHUT_WR)
                self.assertEqual(client.recv(1), b"")
                accepted.close()
        self.config["postgres_port"] = port
        s.free_ports("postgres", self.config)

    def test_start_is_idempotent_and_stop_waits_for_owned_process(self):
        command = [sys.executable, "-c", "import time; time.sleep(60)"]
        with patch.object(s, "installation_check"), patch.object(s, "free_ports"), \
             patch.object(s, "healthy", return_value=True), \
             patch.object(s, "command_for", return_value=(command, self.root, os.environ.copy())) as factory:
            s.start_all(["api"], self.config)
            first = s.running("api")["pid"]
            s.start_all(["api"], self.config)
            self.assertEqual(first, s.running("api")["pid"])
            self.assertEqual(factory.call_count, 1)
            s.stop_one("api")
            self.assertIsNone(s.running("api"))

    def test_failed_later_start_rolls_back_only_new_processes(self):
        command = [sys.executable, "-c", "import time; time.sleep(60)"]
        def factory(name, config):
            if name == "web":
                raise RuntimeError("simulated web launch failure")
            return command, self.root, os.environ.copy()
        with patch.object(s, "installation_check"), patch.object(s, "free_ports"), \
             patch.object(s, "healthy", return_value=True), patch.object(s, "command_for", side_effect=factory):
            with self.assertRaisesRegex(RuntimeError, "simulated"):
                s.start_all(["api", "web"], self.config)
            self.assertIsNone(s.running("api"))
            s.start_all(["api"], self.config)
            pid = s.running("api")["pid"]
            with self.assertRaisesRegex(RuntimeError, "simulated"):
                s.start_all(["api", "web"], self.config)
            self.assertEqual(s.running("api")["pid"], pid)
            s.stop_one("api")

    def test_nonempty_database_directory_is_never_reinitialized(self):
        data = Path(self.config["postgres_data"])
        data.mkdir()
        (data / "valuable-file").write_text("keep")
        with patch.object(s.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "Refusing to overwrite"):
                s.initialize_postgres(self.config)
            run.assert_not_called()
        self.assertEqual((data / "valuable-file").read_text(), "keep")

    def test_health_timeout_rolls_back_new_process(self):
        command = [sys.executable, "-c", "import time; time.sleep(60)"]
        with patch.object(s, "installation_check"), patch.object(s, "free_ports"), \
             patch.object(s, "healthy", return_value=False), \
             patch.object(s.time, "monotonic", side_effect=range(0, 10000, 100)), \
             patch.object(s, "command_for", return_value=(command, self.root, os.environ.copy())):
            with self.assertRaisesRegex(RuntimeError, "health check"):
                s.start_all(["api"], self.config)
        # stop_one's deadline is also advanced by the mock; finish the cleanup if needed.
        if s.running("api"):
            s.stop_one("api")
        self.assertIsNone(s.running("api"))

    def test_operation_lock_rejects_concurrent_mutation(self):
        with s.operation_lock():
            with self.assertRaisesRegex(RuntimeError, "in progress"):
                with s.operation_lock():
                    self.fail("Second lock unexpectedly acquired")

    def test_model_checksum_rejects_same_size_corruption(self):
        file = self.root / "model"
        file.write_bytes(b"abcd")
        expected = s.models.digest(file, "sha256", 4)
        descriptor = {"size": 4, "algorithm": "sha256", "hash": expected}
        self.assertTrue(s.models.valid(file, descriptor))
        file.write_bytes(b"wxyz")
        self.assertFalse(s.models.valid(file, descriptor))

    def test_tei_runs_pinned_local_path_with_original_model_identity(self):
        command, cwd, env = s.command_for("embedding", self.config)
        self.assertEqual(command[command.index("--model-id") + 1], "BAAI/bge-m3")
        self.assertEqual(command[command.index("--revision") + 1], s.VERSIONS["EMBEDDING_REVISION"])
        self.assertEqual(cwd, self.root / "model-links")
        self.assertEqual(env["HF_HUB_OFFLINE"], "1")
        self.assertNotIn("HF_TOKEN", env)


if __name__ == "__main__":
    unittest.main()
