import unittest
from pathlib import Path

from scripts.experiment_policy import policy_for


ROOT = Path(__file__).resolve().parents[1]


class ExpansionBusTests(unittest.TestCase):
    def test_cart_a_is_a_separate_top(self) -> None:
        rtl = (ROOT / "experiments/900_expansion_bus/rtl/cart.v").read_text(encoding="utf-8")
        self.assertIn("module cart", rtl)
        self.assertIn("plug_addr", rtl)
        self.assertIn("plug_rdata", rtl)
        self.assertIn('BEL = "MISTRAL_M10K.26.1.0"', rtl)
        self.assertNotIn("16'hD900", rtl)
        policy = policy_for("900_expansion_bus")
        policy.validate_source_text("experiments/900_expansion_bus/rtl/cart.v", rtl)
        self.assertEqual(policy.top, "cart")
        self.assertTrue(policy.synth_only)
        self.assertEqual(policy.yosys_post_synth, "setattr -set FES_SLOT 1 c:*")
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_M10K": 1})
        self.assertFalse(policy.m10k_async_readonly)

    def test_empty_socket_has_primitive_plugs(self) -> None:
        rtl = (ROOT / "experiments/901_plugged_base/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("16'hD901", rtl)
        self.assertIn("plug_addr", rtl)
        self.assertIn("plug_addr[5:0]", rtl)
        self.assertIn("plug_rdata_d", rtl)
        self.assertIn("MISTRAL_FF", rtl)
        self.assertIn('BEL = "MISTRAL_FF.24.1.2"', rtl)
        self.assertIn('BEL = "MISTRAL_FF.28.1.2"', rtl)
        self.assertNotIn("MISTRAL_M10K", rtl)
        policy = policy_for("901_plugged_base")
        policy.validate_source_text("experiments/901_plugged_base/rtl/top.v", rtl)
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {"cyclonev_hps_interface_mpu_general_purpose": 1},
        )
        qsf = (ROOT / "experiments/901_plugged_base/pins.qsf").read_text(encoding="utf-8")
        self.assertIn('FES_RESERVED_RECT "25 1 27 16"', qsf)
        mapping = (ROOT / "experiments/901_plugged_base/link.toml").read_text(encoding="utf-8")
        self.assertIn('overlay_mode = "cram_rect"', mapping)
        self.assertIn("require_slot_only = true", mapping)
        self.assertIn("x0 = 1769", mapping)
        self.assertIn("x1 = 2806", mapping)

    def test_cart_b_occupies_several_slot_cells(self) -> None:
        rtl = (ROOT / "experiments/903_wide_cart/rtl/cart.v").read_text(encoding="utf-8")
        self.assertIn("module cart", rtl)
        self.assertIn("slot_cell0", rtl)
        self.assertIn("slot_cell3", rtl)
        self.assertIn("plug_addr[11:10]", rtl)
        policy = policy_for("903_wide_cart")
        policy.validate_source_text("experiments/903_wide_cart/rtl/cart.v", rtl)
        self.assertTrue(policy.synth_only)
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_M10K": 4})
        self.assertFalse(policy.m10k_async_readonly)
        self.assertEqual(policy.yosys_post_synth, "setattr -set FES_SLOT 1 c:*")
