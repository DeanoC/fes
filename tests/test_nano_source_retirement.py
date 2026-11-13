"""Sanitized retirement models use active controllers and injected hardware."""
import json
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import sys

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"
FIXTURE = FW / "tests/sdm_retire"


class SourceRetirementTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.output = Path(cls.tmp.name)
        sys.path.insert(0, str(ROOT / "platform/embedded/nano"))
        import usb_readiness
        project = cls.output / "project"
        handoff = project / "board/hps/project/output_files/hps_bootloader_handoff.bin"
        handoff.parent.mkdir(parents=True)
        data = (ROOT / "tests/fixtures/nano-usb-board/hps_bootloader_handoff.bin").read_bytes()
        handoff.write_bytes(data)
        (project / "board/hps/build.json").write_text(json.dumps({"generated_handoff_and_sdc_sha256": {
            "output_files/hps_bootloader_handoff.bin": hashlib.sha256(data).hexdigest()}}))
        (cls.output / "usb_board_policy.h").write_text(usb_readiness.header(project, {"hps_io_hash": "d" * 64}))
        # The input-enabled vendor export installs this selected pair under
        # the vendor's generic setup names. Keep arm-only sources independent.
        for suffix in ("c", "h"):
            shutil.copyfile(FW / ("sdm_input_probe_setup." + suffix), cls.output / ("probe_setup." + suffix))
        shutil.copyfile(FW / "nano_sdm_source_platform.inc", cls.output / "nano_sdm_source_platform.inc")

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def run_model(self, name, joined=False, arguments=()):
        binary = self.output / name
        sources = [FIXTURE / (name + ".c"), FW / "source_retire.c", FW / "command_queue.c", FW / "input_tables.c"]
        if joined:
            sources += [self.output / "probe_setup.c", FW / "input_transfer.c"]
            sources += [FW / (n + ".c") for n in ("nano_cold", "nano_hps_io", "nano_bridges", "nano_boot", "nano_sd", "nano_sd_platform", "nano_usb_platform", "nano_usb_board", "nano_mac", "nano_usb_service", "nano_usb_composition", "nano_configuration")]
        command = [shutil.which("cc") or "cc", "-std=c11", "-O1", "-g", "-Wall", "-Wextra", "-Werror",
                   "-Wno-misleading-indentation", "-Wno-unused-function", "-Wno-unused-variable", "-Wno-sign-compare",
                   "-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-DFES_NANO_RETIRE_BATCHED_OBSERVE=1"]
        if joined:
            command += ["-DFES_NANO_RETIRE_TIMING=1", "-DFES_NANO_SDM_RETIRE_SOURCE=1"]
        command += ["-I" + str(p) for p in (self.output, FW, FW / "tests")]
        if not binary.exists():
            compiled = subprocess.run(command + [str(p) for p in sources] + ["-o", str(binary)], timeout=90, capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
        result = subprocess.run([str(binary), *map(str, arguments)], timeout=180, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout.splitlines()[-1])
        expected = "passed-startup-then-owned-full-transfer" if name == "joined_test" else "passed"
        self.assertEqual(report["status"], expected)
        self.assertFalse(report["hardware_actions"])
        return report

    def test_portable_failures(self):
        self.run_model("retire_test")

    def test_fresh_prewrite_gates(self):
        self.run_model("write_gate_test")

    def test_joined_launch_retirement_failures(self):
        self.run_model("joined_test", joined=True)

    def test_counter_boundaries(self):
        self.run_model("joined_boundary_test", joined=True)

    def test_batch_callback_and_table_faults(self):
        self.run_model("batch_faults", joined=True)

    def test_command_snapshot_callback_faults(self):
        self.run_model("command_snapshot_test")

    def test_command_snapshot_cost_and_unchanged_deadlines(self):
        baseline = json.loads((ROOT / "tests/fixtures/nano-sdm-input/retirement-cost-v6.json").read_text())
        snapshot_baseline = json.loads((ROOT / "tests/fixtures/nano-sdm-input/retirement-cost-v7.json").read_text())
        rows = {}
        for cost in (0, 1, 2, 4, 8, 16, 32, 64, 72, 80, 96, 112, 128, 256, 1000):
            rows[cost] = self.run_model("retire_cost_test", joined=True, arguments=(cost, 0))
        zero = rows[0]
        self.assertTrue(zero["retired"])
        self.assertEqual(zero["writes"], 5)
        self.assertEqual(sum(r[2] for r in zero["phase_counts"]), 25600)
        self.assertLess(zero["phase_counts"][4][0], baseline["phase_counts"][4][0])
        self.assertLess(zero["phase_counts"][4][0], snapshot_baseline["phase_counts"][4][0])
        self.assertLess(zero["phase_counts"][4][5], snapshot_baseline["phase_counts"][4][5])
        for phase in (0, 1, 2, 3, 5, 6):
            self.assertEqual(zero["phase_counts"][phase], snapshot_baseline["phase_counts"][phase])
        self.assertFalse(baseline["eight_us_read_cost_retired"])
        self.assertTrue(rows[8]["retired"])
        self.assertFalse(rows[1000]["retired"])
        slow_clock = self.run_model("retire_cost_test", joined=True, arguments=(0, 1000))
        self.assertFalse(slow_clock["retired"])
        self.cost_observations = {"read_cost_cases": rows, "slow_clock": slow_clock}
