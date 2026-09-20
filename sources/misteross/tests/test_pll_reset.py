import unittest
from pathlib import Path
from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class PllResetTests(unittest.TestCase):
    def test_reset_experiment_policy(self):
        policy = policy_for('110_pll_reset')
        for relative in policy.all_source_paths:
            policy.validate_source_text(relative, (ROOT / relative).read_text())
        self.assertEqual(policy.additional_clocks_mhz, {'clk25': 25.0})
        self.assertEqual(policy.clock_evidence_names, ('meter.refclk',))
        self.assertEqual(policy.required_synth_cells, {'altera_pll': 1})
        policy.validate_resources({'altera_pll': {'used': 1},
                                  'cyclonev_hps_interface_mpu_general_purpose': {'used': 1}})
        with self.assertRaises(PolicyError):
            policy.validate_resources({'altera_pll': {'used': 0},
                                       'cyclonev_hps_interface_mpu_general_purpose': {'used': 1}})
