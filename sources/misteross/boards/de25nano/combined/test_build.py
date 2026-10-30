"""Composition guards using the real retained HPS/display producer implementations."""
from pathlib import Path
import json
import tempfile
import unittest
from unittest import mock
import zipfile
import importlib.util

SPEC = importlib.util.spec_from_file_location("de25_combined_builder_tests", Path(__file__).with_name("build.py"))
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class CombinedBuildTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="de25-combined-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.hps = B.load(B.HERE.parent / "hps/test_build.py", "combined_hps_fixtures")
        display_tests = B.load(B.HERE.parent / "display/test_build.py", "combined_display_fixtures")
        fixture = display_tests.DisplayBuildTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.sources = B.capture()
        self.files = self.hps.vendor_files()
        self.files["golden_top.qsf"] += fixture.baseline.split(b"\n", 1)[1]
        self.files["golden_top.v"] = self.files["golden_top.v"].replace(
            b"qsys_top soc_inst (", b"qsys_top soc_inst (\n.ninit_done_ninit_done                     (ninit_done),")
        self.archive = self.root / "vendor.zip"
        with zipfile.ZipFile(self.archive, "w") as z:
            for name, data in self.files.items():
                z.writestr(self.hps.PREFIX + name, data)
            for name, data in fixture.files.items():
                z.writestr("Demonstration/FPGA/HDMI_ASx4/" + name, data)
        for prefix in (B.HPS, B.DISPLAY):
            key = prefix + "vendor.lock.toml"
            lock = B.tomllib.loads(self.sources[key].decode())
            self.sources[key] = self.sources[key].replace(lock["archive"]["sha256"].encode(), B.sha(self.archive.read_bytes()).encode())

    def prepare(self, mode="idle", revision=None):
        with mock.patch.object(B, "capture", return_value=self.sources):
            return B.prepare(self.archive, mode, self.root / mode, revision)

    def test_prepares_both_real_modes_preserving_hps_and_explicit_inactive_ack(self):
        for mode in ("idle", "menu-pattern"):
            output, hps, _ = self.prepare(mode)
            project = output / "hps/project"
            inputs = json.loads((output / "hps/inputs.json").read_text())
            top = (project / "golden_top.v").read_text()
            self.assertIn(".h2f_warm_reset_handshake_reset_ack         (1'b1)", top)
            selected_pattern = str(int(mode == "menu-pattern"))
            self.assertIn(".MENU_PATTERN(1'b" + selected_pattern, top)
            self.assertEqual((project / self.hps.HPS).read_bytes(), self.files[self.hps.HPS])
            self.assertEqual((project / self.hps.EMIF).read_bytes(), self.files[self.hps.EMIF])
            self.assertEqual((project / self.hps.BOOT).read_bytes(), self.files[self.hps.BOOT])
            self.assertIn("fes_ddr_probe.avm", (project / "integrate-mailbox.tcl").read_text())
            self.assertNotIn("agilex_reset_release", (project / "fes_combined/video.v").read_text())
            self.assertIn("input wire ninit_done", (project / "fes_combined/video.v").read_text())
            self.assertEqual(inputs["combined_display"]["ddr_scanout"], False)
            self.assertEqual(set(hps.fes_sources()), set(inputs["source_sha256"]))
            qsf = (project / "golden_top.qsf").read_text()
            self.assertEqual("fes_combined/fes_menu_reader_de25.v" in qsf, mode == "menu-pattern")
            self.assertEqual("fes_combined/fes_splash_core.v" in qsf, mode == "idle")
            if mode == "menu-pattern":
                self.assertIn(b'ramstyle = "M20K"', (project / "fes_combined/fes_menu_reader_de25.v").read_bytes())

    def test_selected_source_snapshot_is_immutable_and_receipted(self):
        revision = "a" * 40
        output, hps, _ = self.prepare(revision=revision)
        copied = output / "functional-source" / B.HPS / "fes_de25_ddr_probe.v"
        original = copied.read_bytes()
        self.sources[B.HPS + "fes_de25_ddr_probe.v"] = b"unselected edit"
        self.assertEqual(hps.fes_sources()["fes_de25_ddr_probe.v"], original)
        self.assertEqual((output / "hps/project/fes_mailbox/fes_de25_ddr_probe.v").read_bytes(), original)
        inputs = json.loads((output / "hps/inputs.json").read_text())
        self.assertEqual(inputs["fes_revision"], revision)
        self.assertNotIn("base_revision", inputs)
        self.assertEqual(inputs["functional_snapshot_sha256"][B.HPS + "fes_de25_ddr_probe.v"], B.sha(original))

    def test_pin_change_fails_closed(self):
        self.files["golden_top.qsf"] = self.files["golden_top.qsf"].replace(b"PIN_TEST0", b"PIN_WRONG")
        # Build another archive with this changed GHRD while retaining display pins.
        with zipfile.ZipFile(self.archive) as z:
            entries = {name: z.read(name) for name in z.namelist()}
        entries[self.hps.PREFIX + "golden_top.qsf"] = self.files["golden_top.qsf"]
        before = B.sha(self.archive.read_bytes()).encode()
        with zipfile.ZipFile(self.archive, "w") as z:
            for name, data in entries.items(): z.writestr(name, data)
        for prefix in (B.HPS, B.DISPLAY):
            key = prefix + "vendor.lock.toml"
            self.sources[key] = self.sources[key].replace(before, B.sha(self.archive.read_bytes()).encode())
        with self.assertRaisesRegex(ValueError, "pin/IO"):
            self.prepare()

    def test_display_and_top_derivation_reject_changed_targets(self):
        for before in (b"module de25nano_display #(", b"input wire CLOCK0_50,", b"wire ninit_done, pixel_clk,",
                       b"agilex_reset_release configuration_release (.ninit_done(ninit_done));"):
            with self.subTest(before=before), self.assertRaisesRegex(ValueError, "patch target changed"):
                B.derive_video(self.sources[B.DISPLAY + "top.v"].replace(before, b"changed"))
        with self.assertRaisesRegex(ValueError, "warm-reset"):
            B.attach_video("module golden_top; endmodule", "idle")
        with self.assertRaisesRegex(ValueError, "warm-reset"):
            B.attach_video(".ninit_done_ninit_done                     (ninit_done),\n// h2f_warm_reset_handshake\nendmodule", "idle")

    def test_constraints_keep_exact_child_clocks_and_clear_only_exceptions(self):
        constraints = B.derive_constraints(self.sources[B.DISPLAY + "clocks.sdc"].decode())
        self.assertIn("fes_video_inst|hdmi_config|mI2C_CTRL_CLK", constraints)
        self.assertIn("-source [get_ports CLOCK0_50] -divide_by 5002", constraints)
        self.assertIn("fes_video_inst|pixel_reset|sync_regs*|aclr", constraints)
        self.assertIn("$display_reset_pins != 4", constraints)
        self.assertNotIn("set_output_delay", constraints)
        self.assertNotIn("set_clock_groups", constraints)
        with self.assertRaisesRegex(ValueError, "constraint target changed"):
            B.derive_constraints(constraints)

    def test_archive_must_match_both_locked_consumers(self):
        key = B.DISPLAY + "vendor.lock.toml"
        self.sources[key] = self.sources[key].replace(B.sha(self.archive.read_bytes()).encode(), b"f" * 64)
        with self.assertRaisesRegex(ValueError, "selected HPS/display lock"):
            self.prepare()

    def test_i2c_clock_binding_rejects_changed_frequency_or_divider(self):
        reference = b"""parameter CLK_Freq = 50000000;
parameter I2C_Freq = 20000;
if( mI2C_CLK_DIV < (CLK_Freq/I2C_Freq) ) mI2C_CLK_DIV <= mI2C_CLK_DIV+1;
else begin mI2C_CLK_DIV <= 0; mI2C_CTRL_CLK <= ~mI2C_CTRL_CLK; end
"""
        B.verify_i2c_clock(reference)
        for before, after in ((b"50000000", b"40000000"), (b"20000;", b"10000;"),
                              (b" < ", b" <= "), (b"~mI2C_CTRL_CLK", b"mI2C_CTRL_CLK")):
            with self.subTest(before=before), self.assertRaisesRegex(ValueError, "I2C clock divider"):
                B.verify_i2c_clock(reference.replace(before, after))


if __name__ == "__main__":
    unittest.main()
