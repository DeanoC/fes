"""Fail closed when actual generated parent startup/reset connections drift."""
import gzip
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("scanout_startup_test", ROOT / "boards/de25nano/scanout/startup.py")
S = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(S)


class StartupInputsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.project = Path(self.temp.name)
        fixture = json.loads(gzip.decompress((ROOT / "tests/fixtures/de25_startup_reset_inputs.json.gz").read_bytes()))
        for name, text in fixture["files"].items():
            path = self.project / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)

    def mutate(self, name, before, after):
        path = self.project / name
        text = path.read_text()
        self.assertIn(before, text)
        path.write_text(text.replace(before, after, 1))

    def test_real_generated_top_binds_five_controllers_and_separate_sources(self):
        model = S.Model(self.project)
        metadata = model.metadata()
        self.assertEqual(len(metadata["input_sha256"]), 6)
        self.assertEqual(metadata["peripheral_reset_bindings"]["reset_reset_n"], "rst_in_out_reset_reset")
        self.assertEqual(metadata["memory_reset_bindings"]["subsys_periph_fes_scanout_mem_translator_reset_reset_bridge_in_reset_reset"],
                         "rst_controller_003_reset_out_reset")
        self.assertFalse(metadata["global_reset_or_boot_epoch_acceptance"])
        model.verify()

    def test_rejects_parent_top_not_selected_by_qip(self):
        self.mutate(S.QIP, '"synth/qsys_top.v"', '"synth/other_top.v"')
        with self.assertRaisesRegex(ValueError, "not selected"):
            S.Model(self.project)

    def test_rejects_changed_reset_bridge_behavior(self):
        self.mutate(S.BRIDGES[0], "assign out_reset_n = in_reset_n;", "assign out_reset_n = ~in_reset_n;")
        with self.assertRaisesRegex(ValueError, "passthrough"):
            S.Model(self.project)

    def test_rejects_controller_mode_or_clock_drift(self):
        path = self.project / S.TOP
        original = path.read_text()
        for before, after, error in (
            ('("both")', '("none")', "parameters"),
            ('.clk            (clk_100_out_clk_clk)', '.clk            (fes_scanout_pixel_clock_out_clk_clk)', "connections"),
        ):
            with self.subTest(change=before):
                self.mutate(S.TOP, before, after)
                with self.assertRaisesRegex(ValueError, error):
                    S.Model(self.project)
                path.write_text(original)

    def test_rejects_common_reset_substitution_at_memory_boundary(self):
        before = ".subsys_periph_fes_scanout_mem_translator_reset_reset_bridge_in_reset_reset           (rst_controller_003_reset_out_reset)"
        self.mutate(S.TOP, before, before.replace("rst_controller_003", "rst_controller_004"))
        with self.assertRaisesRegex(ValueError, "memory translator"):
            S.Model(self.project)

    def test_rejects_changed_bound_inputs_and_stale_wrapper(self):
        model = S.Model(self.project)
        path = self.project / S.GENERICS[0]
        path.write_bytes(path.read_bytes() + b"\n")
        with self.assertRaisesRegex(ValueError, "changed during"):
            model.verify()
        with self.assertRaisesRegex(ValueError, "wrapper connection changed"):
            model.wrapper("module stale; endmodule")


if __name__ == "__main__":
    unittest.main()
