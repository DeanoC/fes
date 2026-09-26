import stat
import tempfile
import unittest
from pathlib import Path

from scripts.build_fes_slot import SlotBuildError, _require_scaffold_nextpnr
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
        self.assertFalse((ROOT / "experiments/900_expansion_bus/hardware/probe.sh").exists())
        cart_probe = (ROOT / "experiments/901_plugged_base/hardware/probe_cart.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("55553", cart_probe)
        ram16_probe = (ROOT / "experiments/904_zx81_socket/hardware/probe_ram16.sh").read_text(
            encoding="utf-8"
        )
        zonx_probe = (ROOT / "experiments/904_zx81_socket/hardware/probe_zonx.sh").read_text(
            encoding="utf-8"
        )
        qs_probe = (ROOT / "experiments/904_zx81_socket/hardware/probe_qs_chrs.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("55556", ram16_probe)
        self.assertIn("55556", zonx_probe)
        self.assertIn("55556", qs_probe)
        self.assertIn("0x13579BDF", ram16_probe)
        self.assertIn("0x13579BDF", zonx_probe)
        self.assertIn("0x13579BDF", qs_probe)
        self.assertIn("0x4000", ram16_probe)
        self.assertIn("0x00df", zonx_probe)
        self.assertIn("0x8400", qs_probe)

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

    def test_zx81_socket_adds_write_plugs(self) -> None:
        rtl = (ROOT / "experiments/904_zx81_socket/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("16'hD904", rtl)
        self.assertIn("plug_wdata", rtl)
        self.assertIn("plug_mem_we", rtl)
        self.assertIn("plug_io_we", rtl)
        self.assertIn("plug_io_rd", rtl)
        self.assertIn('BEL = "MISTRAL_FF.23.1.2"', rtl)
        self.assertIn('BEL = "MISTRAL_FF.29.1.2"', rtl)
        self.assertNotIn("MISTRAL_M10K", rtl)
        policy = policy_for("904_zx81_socket")
        policy.validate_source_text("experiments/904_zx81_socket/rtl/top.v", rtl)
        qsf = (ROOT / "experiments/904_zx81_socket/pins.qsf").read_text(encoding="utf-8")
        self.assertIn('FES_RESERVED_RECT "25 1 27 32"', qsf)
        mapping = (ROOT / "experiments/904_zx81_socket/link.toml").read_text(encoding="utf-8")
        self.assertIn('overlay_mode = "cram_rect"', mapping)
        self.assertIn("require_slot_only = true", mapping)
        self.assertIn("x0 = 1617", mapping)
        self.assertIn("x1 = 3356", mapping)
        vacant_probe = (ROOT / "experiments/904_zx81_socket/hardware/probe.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("55556", vacant_probe)

    def test_zx81_16k_pack_occupies_sixteen_slot_cells(self) -> None:
        rtl = (ROOT / "experiments/905_zx81_ram16/rtl/cart.v").read_text(encoding="utf-8")
        self.assertIn("plug_addr[15:14]", rtl)
        self.assertIn("slot_cell15", rtl)
        policy = policy_for("905_zx81_ram16")
        policy.validate_source_text("experiments/905_zx81_ram16/rtl/cart.v", rtl)
        self.assertTrue(policy.synth_only)
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_M10K": 16})

    def test_zx81_zonx_decodes_ay_ports(self) -> None:
        rtl = (ROOT / "experiments/906_zx81_zonx/rtl/cart.v").read_text(encoding="utf-8")
        self.assertIn("8'h8f", rtl)
        self.assertIn("8'h0f", rtl)
        self.assertIn("CFG_MIXED_WIDTH", rtl)
        self.assertIn('BEL = "MISTRAL_M10K.26.1.0"', rtl)
        self.assertIn("sel_rd", rtl)
        self.assertIn("ay_sel", rtl)
        policy = policy_for("906_zx81_zonx")
        policy.validate_source_text("experiments/906_zx81_zonx/rtl/cart.v", rtl)
        self.assertTrue(policy.synth_only)
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_M10K": 2})

    def test_zx81_qs_chrs_window(self) -> None:
        rtl = (ROOT / "experiments/907_zx81_qs_chrs/rtl/cart.v").read_text(encoding="utf-8")
        self.assertIn("6'h21", rtl)
        self.assertIn('BEL = "MISTRAL_M10K.26.1.0"', rtl)
        policy = policy_for("907_zx81_qs_chrs")
        policy.validate_source_text("experiments/907_zx81_qs_chrs/rtl/cart.v", rtl)
        self.assertTrue(policy.synth_only)
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_M10K": 1})

    def test_compose_rejects_nextpnr_without_scaffold_flags(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            fake = Path(temporary) / "nextpnr-mistral"
            fake.write_text("#!/bin/sh\necho 'Usage: nextpnr-mistral [options]'\n", encoding="utf-8")
            fake.chmod(fake.stat().st_mode | stat.S_IEXEC)
            with self.assertRaisesRegex(SlotBuildError, "--fes-scaffold/--fes-cart"):
                _require_scaffold_nextpnr(fake)

    def test_readme_points_cart_a_at_oss_synth(self) -> None:
        readme = (ROOT / "README.md").read_text(encoding="utf-8")
        self.assertIn("`make oss EXP=900_expansion_bus`", readme)
        self.assertIn("scripts/build_fes_slot.py", readme)
        self.assertIn("## Freeze-scaffold cartridges", readme)
        self.assertIn("NEXTPNR_MISTRAL", readme)
        self.assertNotIn("Synth-only: `make sim EXP=900_expansion_bus`", readme)
        self.assertNotIn("lacks `--fes-scaffold`", readme)
        lock = (ROOT / "toolchain.lock").read_text(encoding="utf-8")
        self.assertIn("f65075bbc253b7c99e8b96169f4bc85320038329", lock)
        self.assertIn("freeze-scaffold", lock)
        for relative in (
            "experiments/900_expansion_bus/expected.md",
            "experiments/901_plugged_base/expected.md",
            "experiments/903_wide_cart/expected.md",
            "experiments/904_zx81_socket/expected.md",
            "experiments/905_zx81_ram16/expected.md",
            "experiments/906_zx81_zonx/expected.md",
            "experiments/907_zx81_qs_chrs/expected.md",
        ):
            self.assertIn("build_fes_slot.py", (ROOT / relative).read_text(encoding="utf-8"))
