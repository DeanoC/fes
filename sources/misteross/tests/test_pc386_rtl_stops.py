# SPDX-License-Identifier: MIT
"""The pc386 stop regression distinguishes model uncertainty from failures."""
import copy
import unittest

from scripts import sim_fes_pc386 as sim


class RtlStopsReportTest(unittest.TestCase):
    def setUp(self):
        tail = list(sim.RTL_STOP_LINES)
        self.lines = {'oracle': ['R 0010 46', *tail], 'rtl': ['R 00f0 46', *tail]}
        stops = [
            ('F1', '0000:00007c00', 'f1'),
            ('F1', '0000:00007c01', 'f1'),
            ('0F07', '0000:00007c10', '0f07'),
            ('EA', '0008:00007c20', 'ea00001800'),
            ('EA', '0020:00007c30', 'ea00002800'),
            ('F1', '0020:00407c40', 'f1'),
        ]
        self.rtl = {
            'stops': [{'stem': stem, 'site': site, 'bytes': code} for stem, site, code in stops],
            'gaps': [
                {'kind': 'unimplemented', 'stem': 'F1', 'stops': 3, 'sites': 3, 'executed': 3},
                {'kind': 'unimplemented', 'stem': '0F07', 'stops': 1, 'sites': 1, 'executed': 0},
                {'kind': 'unimplemented', 'stem': 'EA', 'stops': 2, 'sites': 2, 'executed': 2},
            ],
            'pmode': [{'steps': 1}, {'steps': 10}, {'steps': 4}],
        }

    def test_only_unmeasured_cr0_reserved_bits_may_differ(self):
        self.assertEqual(sim.rtl_stop_failures(self.lines, self.rtl), [])
        for replacement in ('R 01f0 46', 'R 00f1 46', 'R 00f0 02', 'bad result'):
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
        rtl['stops'].pop()
        self.assertTrue(sim.rtl_stop_failures(self.lines, rtl))
        rtl = copy.deepcopy(self.rtl)
        rtl['pmode'][1]['steps'] = 9
        self.assertTrue(sim.rtl_stop_failures(self.lines, rtl))
        rtl = copy.deepcopy(self.rtl)
        rtl['pmode'][-1]['steps'] = 2
        self.assertTrue(sim.rtl_stop_failures(self.lines, rtl))
        for marker in sim.RTL_STOP_LINES:
            with self.subTest(marker=marker):
                lines = copy.deepcopy(self.lines)
                lines['rtl'].remove(marker)
                self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))


if __name__ == '__main__':
    unittest.main()
