import subprocess
import unittest
from pathlib import Path

from scripts.check_deadcode import MODULES, U1000

ROOT = Path(__file__).resolve().parent.parent


class CheckDeadcodeTests(unittest.TestCase):
    def test_every_tracked_go_module_is_checked(self):
        tracked = subprocess.run(['git', 'ls-files', '*go.mod'], cwd=ROOT, check=True,
                                 capture_output=True, text=True).stdout.split()
        self.assertEqual(sorted(str(Path(p).parent) for p in tracked), sorted(MODULES))

    def test_u1000_line_parse(self):
        match = U1000.match('ui/tenfoot/stamp.go:75:6: type flightMemory is unused (U1000)')
        self.assertEqual(match.groups(), ('ui/tenfoot/stamp.go', '75', '6', 'type flightMemory is unused'))
        self.assertIsNone(U1000.match('-: module requires at least go1.26.5 (compile)'))


if __name__ == '__main__':
    unittest.main()
