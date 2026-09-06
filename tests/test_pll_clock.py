import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllClockTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for('090_pll_clock')
        for relative in policy.all_source_paths:
            policy.validate_source_text(relative, (ROOT / relative).read_text())
        resources = {'altera_pll': {'used': 1},
                     'cyclonev_hps_interface_mpu_general_purpose': {'used': 1}}
        policy.validate_resources(resources)
        for resource in ('MISTRAL_MUL9X9', 'MISTRAL_M10K', 'MISTRAL_MLAB'):
            with self.assertRaises(PolicyError):
                policy.validate_resources({**resources, resource: {'used': 1}})
        for count in (0, 2):
            with self.assertRaises(PolicyError):
                policy.validate_resources({**resources, 'altera_pll': {'used': count}})

    def test_existing_policy_serialization_stays_unchanged(self):
        for name in ('010_blinky', '020_linux_mailbox', '030_m10k_rom',
                     '040_mlab_ram', '050_lut_mul', '060_dsp_mul',
                     '070_mixed_mem', '080_dsp_mem', '100_dsp_rom'):
            self.assertNotIn('additional_clocks_mhz', policy_for(name).as_dict())

    def test_both_clock_domains_required(self):
        policy = policy_for('090_pll_clock')
        ref = {'constraint': 50, 'achieved': 200}
        generated = {'constraint': 25, 'achieved': 300}
        cases = [({'meter.refclk': ref, 'clk25': generated}, True),
                 ({'meter.refclk': ref}, False),
                 ({'clk25': generated}, False),
                 ({'meter.refclk': ref, 'clk25_fake': generated}, False),
                 ({'meter.refclk': ref, 'clk25': {'constraint': 50, 'achieved': 300}}, False),
                 ({'meter.refclk': ref, 'clk25': {'constraint': 25, 'achieved': 24}}, False)]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'timing.json'
            for clocks, passing in cases:
                with self.subTest(clocks=clocks):
                    path.write_text(json.dumps({'fmax': clocks}))
                    if passing:
                        self.assertEqual(_timing(path, 50, policy.clock, policy), ('meter.refclk', 200))
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
