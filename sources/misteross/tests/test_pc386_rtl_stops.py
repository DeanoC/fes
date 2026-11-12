# SPDX-License-Identifier: MIT
"""The pc386 stop regression distinguishes model uncertainty from failures."""
import copy
import unittest

from scripts import sim_fes_pc386 as sim


class RtlStopsReportTest(unittest.TestCase):
    def setUp(self):
        tail = ['X 2', 'C fff0', 'P 3', 'R 55 06', 'DONE']
        self.lines = {'oracle': ['R 10 46', *tail], 'rtl': ['R f0 46', *tail]}
        self.rtl = {'gaps': [{'kind': 'unimplemented', 'stem': 'F1', 'stops': 3,
                              'sites': 3, 'executed': 3}],
                    'pmode': [{'steps': 5}]}

    def test_only_unmeasured_cr0_reserved_bits_may_differ(self):
        self.assertEqual(sim.rtl_stop_failures(self.lines, self.rtl), [])
        for replacement in ('R f1 46', 'R f0 02', 'bad result'):
            with self.subTest(replacement=replacement):
                lines = copy.deepcopy(self.lines)
                lines['rtl'][0] = replacement
                self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))

    def test_missing_restart_or_stretch_fails(self):
        for key, value in (('stops', 2), ('sites', 2), ('executed', 2), ('kind', 'stall')):
            with self.subTest(key=key):
                rtl = copy.deepcopy(self.rtl)
                rtl['gaps'][0][key] = value
                self.assertTrue(sim.rtl_stop_failures(self.lines, rtl))
        rtl = copy.deepcopy(self.rtl)
        rtl['pmode'][0]['steps'] = 1
        self.assertTrue(sim.rtl_stop_failures(self.lines, rtl))
        for marker in ('X 2', 'C fff0', 'P 3', 'DONE'):
            with self.subTest(marker=marker):
                lines = copy.deepcopy(self.lines)
                lines['rtl'].remove(marker)
                self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))


if __name__ == '__main__':
    unittest.main()
