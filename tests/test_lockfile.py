import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts import lockfile


ROOT = Path(__file__).resolve().parents[1]
EXPECTED_COMMITS = {
    "yosys": "fca8ca0a5354e52ce0e158bc6e1eed481e590ed8",
    "mistral": "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
    "nextpnr": "cb0dab2da29f50e869327534c90215e569d7430f",
    "verilator": "5e4151e3e0c8ecf11d9845a93495f37a31b2f667",
    "openfpgaloader": "0c5ebaab1fa63c9d9c684abc0b8e68546ea8ea86",
}


def _complete_lock(**overrides: str) -> str:
    tools = {
        "yosys": {
            "repo": "https://github.com/YosysHQ/yosys.git",
            "commit": "1" * 40,
            "order": 10,
            "rationale": "test pin",
        },
        "mistral": {
            "repo": "https://github.com/Ravenslofty/mistral.git",
            "commit": "2" * 40,
            "order": 20,
            "rationale": "test pin",
        },
        "nextpnr": {
            "repo": "https://github.com/YosysHQ/nextpnr.git",
            "commit": "3" * 40,
            "order": 30,
            "rationale": "test pin",
        },
        "verilator": {
            "repo": "https://github.com/verilator/verilator.git",
            "commit": "4" * 40,
            "order": 40,
            "rationale": "test pin",
        },
        "openfpgaloader": {
            "repo": "https://github.com/trabucayre/openFPGALoader.git",
            "commit": "5" * 40,
            "order": 50,
            "rationale": "test pin",
        },
    }
    for key, value in overrides.items():
        tool, field = key.split(".", 1)
        tools[tool][field] = value
    lines = []
    for tool, fields in tools.items():
        lines.append(f"[tool.{tool}]")
        for field, value in fields.items():
            if isinstance(value, str):
                lines.append(f'{field} = "{value}"')
            else:
                lines.append(f"{field} = {value}")
        lines.append("")
    return "\n".join(lines)


class LockfileTests(unittest.TestCase):
    def test_checked_in_lock_is_complete(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertEqual(
            set(lock), {"yosys", "mistral", "nextpnr", "verilator", "openfpgaloader"}
        )
        for pin in lock.values():
            self.assertRegex(pin.commit, r"^[0-9a-f]{40}$")
            self.assertTrue(pin.repo.startswith("https://github.com/"))

    def test_checked_in_lock_has_approved_commits(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertEqual(
            {name: pin.commit for name, pin in lock.items()}, EXPECTED_COMMITS
        )

    def test_mistral_precedes_nextpnr(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertLess(lock["mistral"].order, lock["nextpnr"].order)

    def test_yosys_pin_documents_its_cmake_build_surface(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertIn("CMake", lock["yosys"].rationale)

    def test_mistral_pin_documents_nextpnr_compatibility_revision(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertEqual(
            lock["mistral"].commit,
            "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
        )
        self.assertIn("nextpnr", lock["mistral"].rationale)

    def test_mistral_pin_includes_its_array_header_fix(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertIn("array", lock["mistral"].rationale)

    def test_get_prints_only_requested_commit(self):
        result = subprocess.run(
            [sys.executable, "scripts/lockfile.py", "get", "nextpnr", "commit"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "cb0dab2da29f50e869327534c90215e569d7430f\n")
        self.assertEqual(result.stderr, "")

    def test_cli_invalid_arguments_exit_two_without_traceback(self):
        result = subprocess.run(
            [sys.executable, "scripts/lockfile.py", "get", "unknown", "commit"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        self.assertEqual(result.stderr, "lockfile: unknown tool: unknown\n")
        self.assertNotIn("Traceback", result.stderr)
        self.assertEqual(len(result.stderr.splitlines()), 1)

    def test_cli_validate_sanitizes_escaped_newline_key(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            script_dir = root / "scripts"
            script_dir.mkdir()
            script = script_dir / "lockfile.py"
            shutil.copy2(ROOT / "scripts" / "lockfile.py", script)
            (root / "toolchain.lock").write_text(
                '"bad\\nkey" = 1\n\n' + _complete_lock(), encoding="utf-8"
            )
            result = subprocess.run(
                [sys.executable, str(script), "validate"],
                cwd=root,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 2)
            self.assertEqual(result.stdout, "")
            message = result.stderr
            self.assertEqual(len(message.splitlines()), 1)
            self.assertNotIn("Traceback", message)
            self.assertIn("bad", message)

    def test_symbolic_commit_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "toolchain.lock"
            path.write_text(_complete_lock(**{"yosys.commit": "main"}), encoding="utf-8")
            with self.assertRaises(ValueError):
                lockfile.load_lock(path)

    def test_unknown_top_level_key_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "toolchain.lock"
            path.write_text("version = 1\n\n" + _complete_lock(), encoding="utf-8")
            with self.assertRaises(ValueError):
                lockfile.load_lock(path)

    def test_missing_field_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "toolchain.lock"
            content = _complete_lock().replace('rationale = "test pin"\n\n', "", 1)
            path.write_text(content, encoding="utf-8")
            with self.assertRaises(ValueError):
                lockfile.load_lock(path)

    def test_non_https_repository_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "toolchain.lock"
            path.write_text(
                _complete_lock(**{"yosys.repo": "http://github.com/YosysHQ/yosys.git"}),
                encoding="utf-8",
            )
            with self.assertRaises(ValueError):
                lockfile.load_lock(path)

    def test_duplicate_build_order_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "toolchain.lock"
            path.write_text(_complete_lock(**{"nextpnr.order": 20}), encoding="utf-8")
            with self.assertRaises(ValueError):
                lockfile.load_lock(path)

    def test_validate_returns_errors_without_traceback(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "toolchain.lock"
            path.write_text(_complete_lock(**{"yosys.commit": "main"}), encoding="utf-8")
            errors = lockfile.validate_lock(path)
            self.assertTrue(errors)
            self.assertTrue(any("commit" in error for error in errors))


if __name__ == "__main__":
    unittest.main()
