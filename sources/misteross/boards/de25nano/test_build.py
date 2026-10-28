"""Exercise the real diagnostic project path without requiring licensed tools."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET


BOARD = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25nano_build", BOARD / "build.py")
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class DiagnosticBuildTests(unittest.TestCase):
    def fake_tool(self, directory, version, *, fail=False, artifact=True, ip_fail=False):
        root = Path(directory) / "Quartus Pro"
        tool = root / "quartus/bin/quartus_sh"
        tool.parent.mkdir(parents=True)
        tool.write_text(
            "#!/usr/bin/env python3\n"
            "import pathlib, sys\n"
            f"version = {version!r}\n"
            "if sys.argv[1:] == ['--version']:\n"
            "    print(version)\n"
            "    raise SystemExit(0)\n"
            "if pathlib.Path(sys.argv[0]).name == 'quartus_ipgenerate':\n"
            "    assert sys.argv[1:] == ['--generate_project_ip_files', '--synthesis=verilog', 'de25nano_blinky']\n"
            "    print('synthetic IP generation transcript')\n"
            f"    if {ip_fail!r}: raise SystemExit(6)\n"
            "    pathlib.Path('ip-generated.marker').write_text('generated')\n"
            "    raise SystemExit(0)\n"
            "assert sys.argv[1:] == ['--flow', 'compile', 'de25nano_blinky']\n"
            "assert pathlib.Path('ip-generated.marker').is_file()\n"
            "print('synthetic compiler transcript')\n"
            + ("raise SystemExit(7)\n" if fail else "")
            + ("pathlib.Path('output_files').mkdir()\n"
               "pathlib.Path('output_files/de25nano_blinky.sof').write_bytes(b'test-only SOF')\n"
               if artifact else ""),
            encoding="utf-8",
        )
        tool.chmod(0o755)
        ip_tool = tool.with_name("quartus_ipgenerate")
        ip_tool.write_bytes(tool.read_bytes())
        ip_tool.chmod(0o755)
        return root

    def test_prepare_standalone_project_preserves_old_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "project with spaces"
            BUILD.prepare_project(output)
            qsf = (output / "de25nano_blinky.qsf").read_text()
            self.assertIn("DEVICE A5EB013BB23BE4SCS", qsf)
            self.assertIn("SDC_FILE clocks.sdc", qsf)
            self.assertIn("source pins.qsf", qsf)
            self.assertIn('STRATIXV_CONFIGURATION_SCHEME "ACTIVE SERIAL X4"', qsf)
            self.assertIn("ACTIVE_SERIAL_CLOCK AS_FREQ_125MHZ", qsf)
            self.assertIn("DEVICE_INITIALIZATION_CLOCK OSC_CLK_1_125MHZ", qsf)
            self.assertIn("IP_FILE reset_release.ip", qsf)
            self.assertNotIn("reset_release_model.v", qsf)
            namespace = {"ip": "http://www.accellera.org/XMLSchema/IPXACT/1685-2014"}
            metadata = ET.parse(output / "reset_release.ip").getroot()
            self.assertEqual(metadata.find("ip:version", namespace).text, "19.4.9")
            self.assertIn("A5EB013BB23BE4SCS", (output / "reset_release.ip").read_text())
            for name in BUILD.SOURCES:
                self.assertEqual((output / name).read_bytes(), (BOARD / name).read_bytes())
            with self.assertRaises(FileExistsError):
                BUILD.prepare_project(output)

    def test_reject_missing_or_wrong_compiler(self):
        with self.assertRaisesRegex(BUILD.BuildError, "explicit Quartus"):
            BUILD.locate_quartus("")
        for version in ("Version 17.0.2 Lite Edition", "Version 26.3.0 Lite Edition",
                        "Version 26.3.00 Pro Edition", "Version 26.3.0.1 Pro Edition",
                        "Version 26.3.1 Pro Edition", "Version 25.1.1 Pro Edition"):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as temporary:
                root = self.fake_tool(temporary, version)
                with self.assertRaisesRegex(BUILD.BuildError, "requires Quartus"):
                    BUILD.locate_quartus(str(root))

    def test_compile_retains_logs_and_explicit_diagnostic_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.fake_tool(temporary, "Quartus Prime Version 26.3.0 Build 114 SC Pro Edition")
            result = subprocess.run(
                ["python3", str(BOARD / "build.py"), "build", "--quartus-root", str(root),
                 "--output", str(Path(temporary) / "compiled project")],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            output = Path(temporary) / "compiled project"
            record = json.loads((output / "build.json").read_text())
            self.assertTrue(record["diagnostic_only"])
            self.assertEqual(record["timing_acceptance"], "not-qualified")
            self.assertEqual(record["hardware_acceptance"], "not-run")
            self.assertIn("synthetic compiler transcript", (output / "compile.log").read_text())
            self.assertIn("synthetic IP generation transcript", (output / "ip-generation.log").read_text())
            self.assertIn("--generate_project_ip_files", record["ip_generation_command"])

    def test_compile_fails_without_a_new_sof_and_retains_failed_log(self):
        for fail, artifact in ((True, True), (False, False)):
            with self.subTest(fail=fail), tempfile.TemporaryDirectory() as temporary:
                root = self.fake_tool(temporary, "Version 26.3.0 Pro Edition",
                                      fail=fail, artifact=artifact)
                tool, version = BUILD.locate_quartus(str(root))
                output = BUILD.prepare_project(Path(temporary) / "project")
                with self.assertRaises(BUILD.BuildError):
                    BUILD.compile_project(output, tool, version)
                self.assertFalse((output / "build.json").exists())
                self.assertTrue((output / "compile.log").exists())

    def test_ip_generation_failure_stops_before_compile(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.fake_tool(temporary, "Version 26.3.0 Pro Edition", ip_fail=True)
            tool, version = BUILD.locate_quartus(str(root))
            output = BUILD.prepare_project(Path(temporary) / "project")
            with self.assertRaisesRegex(BUILD.BuildError, "IP generation failed"):
                BUILD.compile_project(output, tool, version)
            self.assertTrue((output / "ip-generation.log").exists())
            self.assertFalse((output / "compile.log").exists())
            self.assertFalse((output / "build.json").exists())


if __name__ == "__main__":
    unittest.main()
