import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]

class MenuFixtureTest(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('menu_fixtures', ROOT / 'scripts/menu_display_fixtures.py')
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)

    def test_rejection_does_not_modify_display_or_configuration(self):
        e = self.module.Endpoint()
        self.assertEqual(e.command(20, 0, 1), (4, True))
        self.assertFalse(e.configured)
        self.assertEqual(e.command(19, 0, 2), (3, True))
        self.assertEqual(e.command(19, 0, 1), (0, False))
        e.command(2, 0, 1)
        e.command(20, 0, 1)
        self.assertEqual(e.command(19, 0, 1), (4, True))
        self.assertEqual(e.command(21, 2, 1), (4, True))
        self.assertEqual(e.displayed, 0)
        self.assertIsNone(e.pending)

    def test_pending_completion_and_sequence_exhaustion(self):
        e = self.module.Endpoint()
        e.command(19, 0, 1); e.command(2, 0, 1); e.command(20, 0, 1)
        e.command(21, 0, 65535); e.command(21, 1, 65535)
        self.assertEqual(e.command(21, 2, 1), (0, False))
        self.assertEqual(e.displayed, 0)
        self.assertEqual(e.command(21, 0, 1), (4, True))
        e.frame_boundary()
        self.assertEqual(e.displayed, 0xffffffff)
        e.command(21, 0, 1); e.command(21, 1, 0)
        self.assertEqual(e.command(21, 2, 0), (4, True))
        self.assertEqual(e.displayed, 0xffffffff)

    def test_snapshots_and_fixture_toggles(self):
        e = self.module.Endpoint()
        self.assertEqual(e.command(18, 11, 0), (4, True))
        e.underflows = 0x1234ffff
        self.assertEqual(e.command(18, 12, 0), (65535, False))
        e.underflows += 1
        self.assertEqual(e.command(18, 13, 0), (0x1234, False))
        fixture = self.module.fixtures()
        toggle = False
        for exchange in fixture['exchanges']:
            first, second = exchange['gpo']
            self.assertEqual(bool(first & 0x80000000), toggle)
            toggle = not toggle
            self.assertEqual(bool(second & 0x80000000), toggle)
            self.assertEqual(bool(exchange['gpi'] & 0x800000), toggle)
            self.assertEqual(first ^ second, 0x80000000)
