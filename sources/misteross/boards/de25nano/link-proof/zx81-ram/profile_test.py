"""Focused checks of actual profile preparation and its guarded derivations."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("profile_test_builder", HERE.parent / "build.py")
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)

class ProfileTests(unittest.TestCase):
    def setUp(self):
        build.PROFILE = build.load_profile(HERE / "profile.py")
    def tearDown(self):
        build.PROFILE = None
    def test_real_sources_derive_only_board_memory_and_peek_capture(self):
        frozen = build.capture()
        with tempfile.TemporaryDirectory() as tmp:
            project = Path(tmp)
            local = project / "fes_link"
            local.mkdir()
            for name in ("shell.v", "fes_de25_pr_control_hw.tcl"):
                (local / name).write_bytes((HERE.parent / "fpga" / name).read_bytes())
            build.PROFILE.prepare(local, project, frozen)
            self.assertIn("(* preserve, dont_merge, noprune *) output reg [19:0] bus_response",
                (local / "personas.v").read_text())
            self.assertNotIn("MISTRAL", (local / "zx81_ram_pack.v").read_text())
            self.assertIn("zx81_dpram", (local / "zx81_ram_pack.v").read_text())
            self.assertIn('intended_device_family = "Agilex 5"', (local / "zx81_dpram.v").read_text())
            self.assertIn('ram_block_type = "M20K"', (local / "zx81_dpram.v").read_text())
            for name in ("zx81_machine.sv",):
                self.assertEqual((local / name).read_bytes(), frozen["files"][build.PROFILE.FILES[name].relative_to(build.PROFILE.ROOT).as_posix()])
            original = frozen["files"][build.PROFILE.FILES["zx8x.hex"].relative_to(build.PROFILE.ROOT).as_posix()]
            padded = (local / "zx8x.hex").read_bytes().split()
            self.assertEqual(padded[:12288], original.split()[:12288])
            self.assertEqual(padded[12288:], [b"00"] * 4096)
            self.assertIn(b"DEPTH = 16384;", (local / "zx8x.mif").read_bytes())
            self.assertIn(b"[3000..3FFF]: 00;", (local / "zx8x.mif").read_bytes())
            self.assertIn("state == RUNNING ? {response_data_q[31:8], memory_peek}", (local / "shell.v").read_text())
            self.assertIn('FIRMWARE = "zx8x.mif"', (local / "static.v").read_text())
    def test_captured_profile_is_executed_and_load_drift_rejected(self):
        selected = build.PROFILE
        build.capture()
        self.assertIsNot(build.PROFILE, selected)
        build.PROFILE.selected_source_bytes += b'\n'
        with self.assertRaisesRegex(build.BuildError, "profile source changed"):
            build.capture()
    def test_mutated_memory_guard_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "derivation changed"):
            build.PROFILE.replace(b'wrong primitive', b'expected', b'replacement', "memory")
    def test_synthesis_preflight_rejects_incomplete_protection_or_inversion(self):
        # Unit controls use the exact column/name spelling observed in the
        # same-part probe. They are separate from real profile synthesis.
        rows = [f"; slot_inst|core|bus_response[{bit}]~reg0 ; yes ; yes ; yes ;" for bit in range(20)]
        with tempfile.TemporaryDirectory() as tmp:
            report = Path(tmp) / "synthesis.rpt"
            report.write_text("\n".join(rows))
            self.assertEqual(len(build.PROFILE.verify_synthesis(report)["protected_response_registers"]), 20)
            bad_reports = (rows[:-1], rows + [rows[-1]],
                rows[:-1] + [rows[-1].replace("yes", "no", 1)],
                rows + ["", "; Inverted Register Statistics ;", "; slot_inst|core|bus_response[19]~reg0 ; 1 ;"])
            for bad in bad_reports:
                report.write_text("\n".join(bad))
                with self.assertRaises(ValueError):
                    build.PROFILE.verify_synthesis(report)

    def test_counter_profile_keeps_four_cycles_and_no_zx81_sources(self):
        build.PROFILE = None
        self.assertEqual(build.INTERFACE["latency_cycles"], 4)
        self.assertFalse(any("zx81-ram" in name for name in build.capture()["files"]))
