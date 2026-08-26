import contextlib
import hashlib
import io
import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import scripts.doctor as doctor


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
        self._write_executable(path, body)

    def _write_executable(self, path, body):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n", encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    def _install_local_tools(
        self,
        *,
        missing=(),
        bad_digest=(),
        scan_output="Bus device vid:pid probe_type manufacturer serial product\n",
        chain_output="",
        chain_exit=1,
        nextpnr_accept=True,
    ):
        local_install = Path(self.tempdir.name) / "local-install"
        local_bin = local_install / "bin"
        local_build = Path(self.tempdir.name) / "local-build"
        pins = doctor._load_pins()
        for command_name, _display_name, _args in doctor.OSS_TOOLS:
            tool = doctor.command_name_to_tool(command_name)
            pin = pins[tool]
            path = local_bin / command_name
            if command_name == "nextpnr-mistral":
                nextpnr_branch = (
                    '  *" --device 5CSEBA6U23I7 --test "*) '
                    + (
                        'printf "Program finished normally.\\n"; exit 0'
                        if nextpnr_accept
                        else 'printf "unsupported device\\n" >&2; exit 1'
                    )
                    + ";;\n"
                )
                body = (
                    'case " $* " in\n'
                    '  *" --version "*) printf "nextpnr-mistral local-test\\n";;\n'
                    + nextpnr_branch
                    + '  *) printf "unsupported invocation\\n" >&2; exit 1;;\n'
                    + "esac"
                )
            elif command_name == "openFPGALoader":
                body = (
                    'case " $* " in\n'
                    '  *" --version "*) printf "openFPGALoader local-test\\n";;\n'
                    '  *" --list-cables "*) printf "usb-blasterII 09fb:6810\\n";;\n'
                    '  *" --list-boards "*) printf "de10nano usb-blasterII Undefined\\n";;\n'
                    '  *" --board de10nano --scan-usb "*) printf "'
                    + scan_output.replace('"', '\\"').replace("\n", "\\n")
                    + '";;\n'
                    '  *" --board de10nano --detect "*) printf "'
                    + chain_output.replace('"', '\\"').replace("\n", "\\n")
                    + '"; exit '
                    + str(chain_exit)
                    + ";;\n"
                    '  *) printf "unsupported invocation\\n" >&2; exit 1;;\n'
                    + "esac"
                )
            elif command_name == "mistral-cv":
                body = 'printf "Model 5CSEBA6U23I7\\n"'
            else:
                body = f'printf "{command_name} local-test\\n"'
            if command_name not in missing:
                self._write_executable(path, body)
                stamp_dir = local_build / tool
                stamp_dir.mkdir(parents=True, exist_ok=True)
                stamp = stamp_dir / f".built-{pin.commit}"
                stamp.write_text(f"commit={pin.commit}\n", encoding="utf-8")
                digest = hashlib.sha256(path.read_bytes()).hexdigest()
                if command_name in bad_digest:
                    digest = "0" * 64
                (stamp_dir / f".digest-{pin.commit}.sha256").write_text(
                    f"{digest}\n", encoding="utf-8"
                )
        return local_install, local_build

    def _run_with_local_tools(self, args, **fixture):
        local_install, local_build = self._install_local_tools(**fixture)
        environment = os.environ.copy()
        environment["PATH"] = str(self.bin_dir)
        environment.pop("QUARTUS_ROOTDIR", None)
        output = io.StringIO()
        with mock.patch.object(doctor, "INSTALL_BIN", local_install / "bin"), mock.patch.object(
            doctor, "TOOLCHAIN_BUILD", local_build
        ), mock.patch.dict(os.environ, environment, clear=True), contextlib.redirect_stdout(output):
            return_code = doctor.main(list(args))
        return return_code, output.getvalue()

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
        return_code, output = self._run_with_local_tools(
            ["--strict", "oss"], missing=("yosys",)
        )

        self.assertNotEqual(return_code, 0)
        self.assertIn("yosys", output.lower())
        self.assertIn("NOT READY", output)

    def test_strict_oss_rejects_ambient_tool_when_local_binary_is_missing(self):
        return_code, output = self._run_with_local_tools(
            ["--strict", "oss"], missing=("yosys",)
        )

        self.assertNotEqual(return_code, 0)
        self.assertIn("repository-local", output)
        self.assertIn("ambient", output)

    def test_strict_oss_rejects_replaced_local_binary_with_bad_digest(self):
        return_code, output = self._run_with_local_tools(
            ["--strict", "oss"], bad_digest=("yosys",)
        )

        self.assertNotEqual(return_code, 0)
        self.assertIn("digest", output.lower())
        self.assertIn("NOT READY", output)

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
        nextpnr = next(check for check in device if check["name"] == "nextpnr device support")
        self.assertEqual(nextpnr["status"], "OK")
        self.assertIn("5CSEBA6U23I7", nextpnr["detail"])

    def test_strict_hardware_accepts_one_board_cable_and_target_chain(self):
        scan = "Bus device vid:pid probe_type manufacturer serial product\n1-2 09fb:6810 usbBlasterII Altera DE10-Nano\n"
        chain = "JTAG chain:\nindex 0: idcode 0x02d020dd manufacturer altera family cyclone V Soc model 5CSE*A6/5CSX*6\n"

        return_code, output = self._run_with_local_tools(
            ["--json", "--strict", "hardware"],
            scan_output=scan,
            chain_output=chain,
            chain_exit=0,
        )

        self.assertEqual(return_code, 0, output)
        report = json.loads(output)
        cable = next(check for check in report["hardware"] if check["name"] == "Cable detection")
        target = next(check for check in report["hardware"] if check["name"] == "JTAG chain target")
        nextpnr = next(check for check in report["device"] if check["name"] == "nextpnr device support")
        self.assertEqual(cable["status"], "OK")
        self.assertEqual(target["status"], "OK")
        self.assertEqual(nextpnr["status"], "OK")

    def test_strict_hardware_rejects_wrong_jtag_device(self):
        scan = "Bus device vid:pid probe_type manufacturer serial product\n1-2 09fb:6810 usbBlasterII Altera DE10-Nano\n"
        chain = "JTAG chain:\nindex 0: idcode 0x02d120dd manufacturer altera family cyclone V Soc model 5CSE*A5/5CST*5\n"

        return_code, output = self._run_with_local_tools(
            ["--json", "--strict", "hardware"],
            scan_output=scan,
            chain_output=chain,
            chain_exit=0,
        )

        self.assertNotEqual(return_code, 0)
        target = next(check for check in json.loads(output)["hardware"] if check["name"] == "JTAG chain target")
        self.assertEqual(target["status"], "NOT READY")
        self.assertIn("5CSEBA6U23I7", target["detail"])

    def test_strict_hardware_accepts_exact_alias_without_idcode(self):
        scan = "Bus device vid:pid probe_type manufacturer serial product\n1-2 09fb:6810 usbBlasterII Altera DE10-Nano\n"
        chain = "JTAG chain:\n5CSEBA6U23I7\n"

        return_code, output = self._run_with_local_tools(
            ["--json", "--strict", "hardware"],
            scan_output=scan,
            chain_output=chain,
            chain_exit=0,
        )

        self.assertEqual(return_code, 0, output)
        target = next(check for check in json.loads(output)["hardware"] if check["name"] == "JTAG chain target")
        self.assertEqual(target["status"], "OK")
        self.assertIn("5CSEBA6U23I7", target["detail"])

    def test_strict_hardware_rejects_multiple_jtag_devices(self):
        scan = "Bus device vid:pid probe_type manufacturer serial product\n1-2 09fb:6810 usbBlasterII Altera DE10-Nano\n"
        chain = (
            "JTAG chain:\n"
            "index 0: idcode 0x02d020dd manufacturer altera family cyclone V Soc\n"
            "index 1: idcode 0x02d120dd manufacturer altera family cyclone V Soc\n"
        )

        return_code, output = self._run_with_local_tools(
            ["--json", "--strict", "hardware"],
            scan_output=scan,
            chain_output=chain,
            chain_exit=0,
        )

        self.assertNotEqual(return_code, 0)
        target = next(check for check in json.loads(output)["hardware"] if check["name"] == "JTAG chain target")
        self.assertEqual(target["status"], "NOT READY")
        self.assertIn("multiple", target["detail"])

    def test_strict_hardware_rejects_ambiguous_cable_detection(self):
        scan = (
            "Bus device vid:pid probe_type manufacturer serial product\n"
            "1-2 09fb:6810 usbBlasterII Altera DE10-Nano\n"
            "1-3 09fb:6810 usbBlasterII Altera DE10-Nano\n"
        )
        chain = "JTAG chain:\nindex 0: idcode 0x02d020dd manufacturer altera family cyclone V Soc\n"

        return_code, output = self._run_with_local_tools(
            ["--json", "--strict", "hardware"],
            scan_output=scan,
            chain_output=chain,
            chain_exit=0,
        )

        self.assertNotEqual(return_code, 0)
        cable = next(check for check in json.loads(output)["hardware"] if check["name"] == "Cable detection")
        self.assertEqual(cable["status"], "NOT READY")
        self.assertIn("ambiguous", cable["detail"])

    def test_named_nextpnr_device_check_fails_when_probe_rejects_target(self):
        scan = "Bus device vid:pid probe_type manufacturer serial product\n"
        return_code, output = self._run_with_local_tools(
            ["--json"], scan_output=scan, nextpnr_accept=False
        )

        self.assertEqual(return_code, 0)
        nextpnr = next(check for check in json.loads(output)["device"] if check["name"] == "nextpnr device support")
        self.assertEqual(nextpnr["status"], "NOT READY")
        self.assertIn("5CSEBA6U23I7", nextpnr["detail"])

    def test_timeout_diagnostic_is_distinct_from_launch_failure(self):
        timed_out = doctor.run_command(
            (sys.executable, "-c", "import time; time.sleep(0.2)"), timeout=0.01
        )
        launch_failed = doctor.run_command(("/definitely/missing/open-mister-tool",), timeout=0.1)

        self.assertIsNone(timed_out.completed)
        self.assertIn("timed out after", timed_out.error)
        self.assertIsNone(launch_failed.completed)
        self.assertIn("could not launch", launch_failed.error)


if __name__ == "__main__":
    unittest.main()
