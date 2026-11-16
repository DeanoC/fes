"""Check frozen x87 build inputs and retained failures without licensed tools."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from boards.de25nano.x87 import build


class X87BuildTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="de25-x87-build-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def fake_tools(self):
        directory = self.root / "tools"
        directory.mkdir()
        for name in ("quartus_sh", "quartus_syn", "quartus_fit", "quartus_sta"):
            tool = directory / name
            tool.write_text("#!/bin/sh\nexit 0\n")
            tool.chmod(0o755)
        return directory / "quartus_sh"

    def test_changed_snapshot_is_refused_before_compiler_launch(self):
        output = build.prepare(self.root / "prepared")
        source = output / "project/fes_x87_fast.sv"
        source.write_bytes(source.read_bytes() + b"\n// changed after capture\n")
        with patch.object(build, "run") as compiler:
            with self.assertRaisesRegex(build.BuildError, "prepared project source changed: fes_x87_fast.sv"):
                build.compile_project(output, self.fake_tools(), "test-only")
        compiler.assert_not_called()

    def test_existing_output_is_preserved(self):
        output = self.root / "existing"
        output.mkdir()
        marker = output / "keep.txt"
        marker.write_text("another build owns this directory\n")
        with self.assertRaises(FileExistsError):
            build.prepare(output)
        self.assertEqual(marker.read_text(), "another build owns this directory\n")
        self.assertEqual(list(output.iterdir()), [marker])

    def test_malformed_final_report_keeps_failed_build_record(self):
        output = build.prepare(self.root / "prepared")
        reports = output / "project/output_files"
        reports.mkdir()
        # Synthetic report rows exercise failure handling only. They do not
        # establish resource use or timing acceptance for an FPGA artifact.
        (reports / "fes_de25_x87.fit.rpt").write_text(
            "; Logic utilization (in ALMs) ; 12 / 46,800 ;\n"
            "; Dedicated logic registers ; 22 ;\n"
            "; Total DSP Blocks ; 9 / 188 ;\n"
            "; M20K blocks ; 4 / 358 ;\n"
            "; Total block memory bits ; 1,568 / 7,331,840 ;\n")
        (output / "timing.json").write_text("{ invalid JSON report")
        with patch.object(build, "run") as compiler, patch.object(build, "require_disk"):
            with self.assertRaises(json.JSONDecodeError):
                build.compile_project(output, self.fake_tools(), "test-only")
        self.assertEqual(compiler.call_count, 4)
        record = json.loads((output / "build.json").read_text())
        self.assertEqual(record["status"], "fail")
        self.assertIn("timing_parse_error", record)
        self.assertEqual(record["resources"]["dsp_blocks"]["used"], 9)
        self.assertIsNone(record["programming_artifact"])
        self.assertNotIn("timing", record)


if __name__ == "__main__":
    unittest.main()
