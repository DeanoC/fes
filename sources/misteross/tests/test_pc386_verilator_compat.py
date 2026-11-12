# SPDX-License-Identifier: MIT
"""The pc386 simulation keeps supporting the system and locked Verilators."""
import subprocess
import unittest
from unittest import mock

from scripts import sim_fes_pc386 as sim


class VerilatorCompatibilityTest(unittest.TestCase):
    def tearDown(self):
        sim.verilator_supports_procassinit.cache_clear()

    def test_lint_controls_follow_procassinit_support_not_release_threshold(self):
        for version, probe_status, expected in (('5.032', 1, False), ('5.052', 0, True),
                                                ('5.054', 0, True)):
            with self.subTest(version=version):
                sim.verilator_supports_procassinit.cache_clear()
                version_result = subprocess.CompletedProcess(
                    ['verilator', '--version'], 0, f'Verilator {version}\n', '')
                probe_result = subprocess.CompletedProcess(
                    ['verilator', '-Wno-PROCASSINIT', '--version'], probe_status, '', '')
                with mock.patch.object(sim.subprocess, 'run',
                                       side_effect=[version_result, probe_result]) as run:
                    self.assertEqual(sim.verilator_supports_procassinit('verilator'), expected)
                    self.assertEqual(run.call_args_list[0].args[0], ['verilator', '--version'])
                    run.assert_any_call(
                        ['verilator', '-Wno-PROCASSINIT', '--version'],
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                        timeout=30, check=False)

    def test_missing_or_unresponsive_verilator_is_reported(self):
        sim.verilator_supports_procassinit.cache_clear()
        with mock.patch.object(sim.subprocess, 'run', side_effect=OSError('missing')):
            with self.assertRaises(sim.SimError):
                sim.verilator_supports_procassinit('verilator')
        sim.verilator_supports_procassinit.cache_clear()
        with mock.patch.object(sim.subprocess, 'run', side_effect=subprocess.TimeoutExpired('verilator', 30)):
            with self.assertRaises(sim.SimError):
                sim.verilator_supports_procassinit('verilator')
        sim.verilator_supports_procassinit.cache_clear()
        invalid_version = subprocess.CompletedProcess(['verilator', '--version'], 0, 'unexpected', '')
        with mock.patch.object(sim.subprocess, 'run', return_value=invalid_version):
            with self.assertRaises(sim.SimError):
                sim.verilator_supports_procassinit('verilator')


if __name__ == '__main__':
    unittest.main()
