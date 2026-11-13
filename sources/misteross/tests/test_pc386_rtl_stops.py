# SPDX-License-Identifier: MIT
"""The pc386 stop regression distinguishes model uncertainty from failures."""
import copy
import unittest

from scripts import sim_fes_pc386 as sim


class RtlStopsReportTest(unittest.TestCase):
    def setUp(self):
        tail = list(sim.RTL_STOP_LINES)
        self.lines = {'oracle': ['R 0010 46', *tail], 'rtl': ['R fff0 46', *tail]}
        stops = [
            ('0F07', '0000:00007c10', '0f07'),
            ('EA', '0008:00007c20', 'ea00001800'),
            ('EA', '0020:00007c30', 'ea00002800'),
        ]
        self.rtl = {
            'stops': [{'stem': stem, 'site': site, 'bytes': code} for stem, site, code in stops],
            'gaps': [
                {'kind': 'unimplemented', 'stem': '0F07', 'stops': 1, 'sites': 1, 'executed': 0},
                {'kind': 'unimplemented', 'stem': 'EA', 'stops': 2, 'sites': 2, 'executed': 2},
            ],
            'pmode': [{'steps': 1}, {'steps': 10}],
        }

    def test_post_bios_snapshots_and_lahf_flags(self):
        self.assertEqual(sim.rtl_stop_failures(self.lines, self.rtl), [])
        # No bit of either full-AX software baseline may silently change.
        for cpu, baseline in sim.SMSW_POST_BIOS.items():
            for bit in range(16):
                with self.subTest(cpu=cpu, bit=bit):
                    lines = copy.deepcopy(self.lines)
                    lines[cpu][0] = f'R {baseline ^ (1 << bit):04x} 46'
                    self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))
        for cpu in ('oracle', 'rtl'):
            for replacement in ('R 0000 02', 'bad result'):
                with self.subTest(cpu=cpu, replacement=replacement):
                    lines = copy.deepcopy(self.lines)
                    lines[cpu][0] = replacement
                    self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))
            with self.subTest(cpu=cpu, check='flags'):
                lines = copy.deepcopy(self.lines)
                lines[cpu][0] = lines[cpu][0][:-2] + '02'
                self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))

    def test_missing_restart_or_stretch_fails(self):
        for key, value in (('stops', 2), ('sites', 2), ('executed', 2)):
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
        for marker in sim.RTL_STOP_LINES:
            with self.subTest(marker=marker):
                lines = copy.deepcopy(self.lines)
                lines['rtl'].remove(marker)
                self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))
        for cpu in ('oracle', 'rtl'):
            with self.subTest(cpu=cpu, check='post-write SMSW'):
                lines = copy.deepcopy(self.lines)
                lines[cpu][1] = 'C fff1'
                self.assertTrue(sim.rtl_stop_failures(lines, self.rtl))


if __name__ == '__main__':
    unittest.main()
