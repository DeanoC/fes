import os
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BOOTSTRAP = ROOT / "scripts" / "bootstrap.sh"
ENV = ROOT / "scripts" / "env.sh"
LOCK_COMMITS = {
    "yosys": "13b43f8c85ec430a33ee55d058fb4c32b42b6910",
    "mistral": "bfa096c1deac6180a3eee784693c28dac491ab18",
    "nextpnr": "7d4f72c0aabc15da932748a54e82a6ff7b41921e",
    "verilator": "5e4151e3e0c8ecf11d9845a93495f37a31b2f667",
    "openfpgaloader": "0c5ebaab1fa63c9d9c684abc0b8e68546ea8ea86",
}


class BootstrapInterfaceTests(unittest.TestCase):
    def test_print_plan_lists_pins_in_lock_order_without_creating_build_tree(self):
        build_root = ROOT / "build" / "toolchain"
        before = build_root.stat() if build_root.exists() else None

        with tempfile.TemporaryDirectory() as directory:
            guard_dir = Path(directory)
            marker = guard_dir / "git-invoked"
            guard = guard_dir / "git"
            guard.write_text(f"#!/bin/sh\nprintf '%s\\n' git > {marker}\nexit 99\n")
            guard.chmod(0o755)
            result = subprocess.run(
                [str(BOOTSTRAP), "--print-plan"],
                cwd=ROOT,
                text=True,
                capture_output=True,
                env={"PATH": f"{guard_dir}:{os.environ['PATH']}"},
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(marker.exists(), result.stderr)
        output = result.stdout
        positions = []
        for tool, commit in LOCK_COMMITS.items():
            self.assertIn(commit, output)
            positions.append(output.index(f"tool: {tool}"))
        self.assertEqual(positions, sorted(positions))
        self.assertLess(output.index("tool: mistral"), output.index("tool: nextpnr"))
        self.assertIn("source:", output)
        self.assertIn("build:", output)
        self.assertIn("install:", output)
        after = build_root.stat() if build_root.exists() else None
        self.assertEqual(before is not None, after is not None)
        if before is not None and after is not None:
            self.assertEqual(before.st_mtime_ns, after.st_mtime_ns)

    def test_check_prereqs_reports_without_invoking_package_managers(self):
        marker = ROOT / "build" / "toolchain-prereq-test-marker"
        if marker.exists():
            marker.unlink()
        guard_dir = marker.parent / "prereq-guard-bin"
        guard_dir.mkdir(parents=True, exist_ok=True)
        for command in ("apt", "apt-get", "dnf", "pacman", "sudo"):
            guard = guard_dir / command
            guard.write_text(f"#!/bin/sh\nprintf '%s\\n' {command} > {marker}\nexit 99\n")
            guard.chmod(0o755)
        try:
            result = subprocess.run(
                [str(BOOTSTRAP), "--check-prereqs"],
                cwd=ROOT,
                text=True,
                capture_output=True,
                env={"PATH": f"{guard_dir}:{os.environ['PATH']}"},
            )
            self.assertNotEqual(result.returncode, 99)
            self.assertFalse(marker.exists(), result.stderr)
            self.assertNotIn("executing apt", result.stdout + result.stderr)
            self.assertNotIn("executing dnf", result.stdout + result.stderr)
            self.assertNotIn("executing pacman", result.stdout + result.stderr)
            self.assertNotIn("executing sudo", result.stdout + result.stderr)
        finally:
            for child in guard_dir.iterdir():
                child.unlink()
            guard_dir.rmdir()
            if marker.exists():
                marker.unlink()

    def test_check_prereqs_reports_boost_link_components(self):
        result = subprocess.run(
            [str(BOOTSTRAP), "--check-prereqs"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertIn("Boost components", result.stdout + result.stderr)

    def test_environment_prepends_repository_local_install_bin(self):
        command = (
            "set -e; unset OPEN_MISTER_ROOT; unset TOOLCHAIN_ROOT; "
            f"source {ENV}; "
            "printf '%s\\n' \"$OPEN_MISTER_ROOT\"; "
            "printf '%s\\n' \"${PATH%%:*}\"; "
            "printf '%s\\n' \"$PKG_CONFIG_PATH\""
        )
        result = subprocess.run(
            ["bash", "--noprofile", "--norc", "-c", command],
            cwd=ROOT,
            text=True,
            capture_output=True,
            env={"PATH": os.environ["PATH"]},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(lines[0], str(ROOT))
        self.assertEqual(lines[1], str(ROOT / "build" / "toolchain" / "install" / "bin"))
        self.assertIn(str(ROOT / "build" / "toolchain" / "install" / "lib"), lines[2])
        self.assertNotIn("quartus", result.stdout.lower() + result.stderr.lower())


if __name__ == "__main__":
    unittest.main()
