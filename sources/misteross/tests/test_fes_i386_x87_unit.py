"""fes_i386_x87's bench against the host's x87 (#1768 slice 2)."""
from __future__ import annotations

import unittest

from scripts import test_fes_i386_x87_unit as x87


class ParseTest(unittest.TestCase):
    def test_counts_and_failures(self):
        passed, failed, failures = x87.parse('PASS load-f32-rc0\nFAIL store-i16-rc1 1 of 62 x\nSUMMARY 69 1\n')
        self.assertEqual((passed, failed), (69, 1))
        self.assertEqual(failures, ['FAIL store-i16-rc1 1 of 62 x'])

    def test_no_summary_is_an_error(self):
        with self.assertRaisesRegex(x87.X87UnitError, 'SUMMARY'):
            x87.parse('PASS load-f32-rc0\n')


class SourcesTest(unittest.TestCase):
    def test_the_bench_and_the_unit_are_in_the_tree(self):
        self.assertTrue((x87.SIM / 'x87_unit_tb.cpp').is_file())
        for name in x87.RTL_SOURCES:
            self.assertTrue((x87.RTL / name).is_file(), name)


if __name__ == '__main__':
    unittest.main()
