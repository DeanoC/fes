import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts import lockfile


ROOT = Path(__file__).resolve().parents[1]


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

    def test_mistral_precedes_nextpnr(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertLess(lock["mistral"].order, lock["nextpnr"].order)

    def test_get_prints_only_requested_commit(self):
        result = subprocess.run(
            [sys.executable, "scripts/lockfile.py", "get", "nextpnr", "commit"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "7d4f72c0aabc15da932748a54e82a6ff7b41921e\n")
        self.assertEqual(result.stderr, "")

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
