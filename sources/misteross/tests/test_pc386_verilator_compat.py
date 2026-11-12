# SPDX-License-Identifier: MIT
"""The pc386 simulation keeps supporting the system and locked Verilators."""
import subprocess
import unittest
from unittest import mock

from scripts import sim_fes_pc386 as sim


class VerilatorCompatibilityTest(unittest.TestCase):
    def tearDown(self):
        sim.verilator_uses_5054_waivers.cache_clear()

    def test_only_versions_with_new_lint_receive_its_waivers(self):
        for version, expected in (('5.032 2025-01-01', False), ('5.054 2026-11-01', True),
                                  ('5.100 future', True)):
            with self.subTest(version=version):
                sim.verilator_uses_5054_waivers.cache_clear()
                result = subprocess.CompletedProcess(['verilator', '--version'], 0,
                                                     f'Verilator {version}\n', '')
                with mock.patch.object(sim.subprocess, 'run', return_value=result):
                    self.assertEqual(sim.verilator_uses_5054_waivers('verilator'), expected)

    def test_unknown_or_failed_version_is_not_silently_accepted(self):
        for stdout, status in (('unexpected output', 0), ('Verilator 5.054', 1)):
            with self.subTest(stdout=stdout, status=status):
                sim.verilator_uses_5054_waivers.cache_clear()
                result = subprocess.CompletedProcess(['verilator', '--version'], status, stdout, '')
                with mock.patch.object(sim.subprocess, 'run', return_value=result):
                    with self.assertRaises(sim.SimError):
                        sim.verilator_uses_5054_waivers('verilator')


if __name__ == '__main__':
    unittest.main()
