from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/780_quartus_sdc/rtl/top.v"
SDC = ROOT / "experiments/780_quartus_sdc/clocks.sdc"
QSF = ROOT / "experiments/780_quartus_sdc/pins.qsf"
PROBE = ROOT / "experiments/780_quartus_sdc/hardware/probe.sh"


class QuartusSdcTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for path in (RTL, SDC, QSF, PROBE, ROOT / "experiments/780_quartus_sdc/sim/tb.cpp"):
            self.assertTrue(path.is_file(), path)

    def test_policy_uses_quartus_constraint_files(self) -> None:
        policy = policy_for("780_quartus_sdc")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/780_quartus_sdc/pins.qsf",
                "experiments/780_quartus_sdc/clocks.sdc",
            ),
        )
        self.assertEqual(dict(policy.additional_clocks_mhz), {"clk25": 25.0})
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD780", rtl)
        policy.validate_source_text("experiments/780_quartus_sdc/rtl/top.v", rtl)

    def test_sdc_uses_quartus_compatibility_commands(self) -> None:
        sdc = SDC.read_text(encoding="utf-8")
        self.assertIn("derive_pll_clocks", sdc)
        self.assertIn("derive_clock_uncertainty", sdc)
        self.assertIn("set_clock_groups -asynchronous", sdc)
        self.assertIn("get_clocks {FPGA_CLK1_50}", sdc)
        self.assertIn("get_clocks {clk25}", sdc)
        self.assertIn("\\\n", sdc)

    def test_qsf_uses_entity_qualifier(self) -> None:
        qsf = QSF.read_text(encoding="utf-8")
        self.assertIn("-entity top", qsf)
        self.assertIn("PIN_V11", qsf)

    def test_probe_uses_780_signature(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("D780", probe)
        self.assertIn("55168", probe)

    def test_closed_resources(self) -> None:
        policy = policy_for("780_quartus_sdc")
        resources = {
            "altera_pll": {"used": 1},
            "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
        }
        policy.validate_resources(resources)
        with self.assertRaises(PolicyError):
            policy.validate_resources({**resources, "MISTRAL_M10K": {"used": 1}})
