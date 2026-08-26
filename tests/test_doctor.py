import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DOCTOR = ROOT / "scripts" / "doctor.py"


class DoctorTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.bin_dir = Path(self.tempdir.name) / "bin"
        self.bin_dir.mkdir()
        self._install_shims()

    def tearDown(self):
        self.tempdir.cleanup()

    def _shim(self, name, body):
        path = self.bin_dir / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    def _install_shims(self, missing=()):
        shims = {
            "cmake": 'printf "cmake version 3.30.0\\n"',
            "ninja": 'printf "1.12.1\\n"',
            "make": 'printf "GNU Make 4.4\\n"',
            "cc": 'printf "cc (test) 1.0\\n"',
            "c++": 'printf "c++ (test) 1.0\\n"',
            "git": 'printf "git version 2.45.0\\n"',
            "python3": 'printf "Python 3.13.0\\n"',
            "yosys": 'printf "Yosys test-1\\n"',
            "verilator": 'printf "Verilator test-1\\n"',
            "mistral-cv": 'printf "Mistral CV test-1\\n"',
            "nextpnr-mistral": (
                'case " $* " in\n'
                '  *" --version "*) printf "nextpnr-mistral test-1\\n";;\n'
                '  *" --help "*) printf "--device arg --test\\n";;\n'
                '  *" --device 5CSEBA6U23I7 --test "*) printf "Program finished normally.\\n";;\n'
                '  *) printf "unsupported device\\n" >&2; exit 1;;\n'
                'esac'
            ),
            "openFPGALoader": (
                'case " $* " in\n'
                '  *" --version "*) printf "openFPGALoader test-1\\n";;\n'
                '  *" --list-boards "*) printf "de10nano usb-blasterII 5CSEBA6U23I7\\n";;\n'
                '  *" --list-cables "*) printf "usb-blasterII 09fb:6810\\n";;\n'
                '  *" --detect "*) printf "no cable\\n" >&2; exit 1;;\n'
                '  *) printf "help\\n";;\n'
                'esac'
            ),
            "lsusb": 'printf "Bus 001 Device 001: ID 1d6b:0002 Linux Foundation 2.0 root hub\\n"',
        }
        for name, body in shims.items():
            if name not in missing:
                self._shim(name, body)
            else:
                (self.bin_dir / name).unlink(missing_ok=True)

    def _run(self, *args, extra_env=None):
        env = os.environ.copy()
        env["PATH"] = str(self.bin_dir)
        env.pop("QUARTUS_ROOTDIR", None)
        if extra_env:
            env.update(extra_env)
        return subprocess.run(
            [sys.executable, str(DOCTOR), *args],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
        )

    def test_general_report_is_successful_and_quartus_is_optional(self):
        result = self._run()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("OPTIONAL ORACLE", result.stdout)
        self.assertIn("OPTIONAL MISSING", result.stdout)
        self.assertIn("Quartus", result.stdout)

    def test_strict_oss_fails_when_required_binary_is_absent(self):
        self._install_shims(missing=("yosys",))

        result = self._run("--strict", "oss")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("yosys", result.stdout.lower())
        self.assertIn("MISSING", result.stdout)

    def test_json_has_structured_readiness_groups(self):
        result = self._run("--json")

        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(
            set(report), {"host", "required_oss", "optional_oracle", "hardware", "device"}
        )
        for group in report.values():
            self.assertIsInstance(group, list)
            for check in group:
                self.assertEqual(set(check), {"name", "status", "detail", "required"})

    def test_quartus_is_only_inspected_with_explicit_root(self):
        quartus_root = Path(self.tempdir.name) / "quartus"
        quartus_bin = quartus_root / "bin"
        quartus_bin.mkdir(parents=True)
        marker = Path(self.tempdir.name) / "quartus-called"
        self._shim(
            "quartus_sh",
            f'printf "Quartus Prime 17.0.2\\n"; : > "{marker}"',
        )
        quartus_executable = quartus_bin / "quartus_sh"
        quartus_executable.write_text(
            (self.bin_dir / "quartus_sh").read_text(encoding="utf-8"), encoding="utf-8"
        )
        quartus_executable.chmod(
            quartus_executable.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH
        )

        without_root = self._run()
        self.assertEqual(without_root.returncode, 0, without_root.stderr)
        self.assertFalse(marker.exists())
        self.assertIn("not set", without_root.stdout)

        with_root = self._run("--json", extra_env={"QUARTUS_ROOTDIR": str(quartus_root)})
        self.assertEqual(with_root.returncode, 0, with_root.stderr)
        self.assertTrue(marker.exists())
        oracle = json.loads(with_root.stdout)["optional_oracle"]
        self.assertTrue(any("17.0.2" in check["detail"] for check in oracle))

    def test_device_readiness_requires_exact_nextpnr_device_evidence(self):
        result = self._run("--json")

        self.assertEqual(result.returncode, 0, result.stderr)
        device = json.loads(result.stdout)["device"]
        self.assertTrue(any(check["status"] == "OK" for check in device))
        self.assertTrue(any("5CSEBA6U23I7" in check["detail"] for check in device))


if __name__ == "__main__":
    unittest.main()
