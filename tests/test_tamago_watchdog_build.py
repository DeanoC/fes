"""Tests for opt-in DE25-Nano watchdog build option validation."""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


def _load_build():
    path = ROOT / 'platform/embedded/tamago/build.py'
    spec = importlib.util.spec_from_file_location('fes_tamago_build_wdt_test', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class TamagoWatchdogBuildOptionTests(unittest.TestCase):
    def test_watchdog_option_validation(self):
        build = _load_build()
        # Default (both zero/None) stays strictly disabled on any program.
        self.assertEqual(build.watchdog_option('probe', False, False, 0, 0), (0, 0))
        self.assertEqual(build.watchdog_option('fpgaload', True, True, None, None), (0, 0))

        # Valid opt-in on fpgaload + selected_programs + tenfoot_serial (within 2^31 cycles).
        self.assertEqual(build.watchdog_option('fpgaload', True, True, 10, 100_000_000), (10, 100_000_000))
        self.assertEqual(build.watchdog_option('fpgaload', True, True, 21, 100_000_000), (21, 100_000_000))
        self.assertEqual(build.watchdog_option('fpgaload', True, True, 5, 400_000_000), (5, 400_000_000))
        self.assertEqual(build.watchdog_option('fpgaload', True, True, 60, 25_000_000), (60, 25_000_000))

        # Reject non-fpgaload / missing tenfoot_serial / partial / out-of-range / >2^31 cycles / non-int.
        bad_cases = [
            ('probe', True, True, 10, 100_000_000),
            ('fpgaload', False, True, 10, 100_000_000),
            ('fpgaload', True, False, 10, 100_000_000),
            ('fpgaload', True, True, 10, 0),
            ('fpgaload', True, True, 0, 100_000_000),
            ('fpgaload', True, True, -1, 100_000_000),
            ('fpgaload', True, True, 61, 25_000_000),
            ('fpgaload', True, True, 10, 999_999),
            ('fpgaload', True, True, 10, 400_000_001),
            ('fpgaload', True, True, 22, 100_000_000),
            ('fpgaload', True, True, 6, 400_000_000),
            ('fpgaload', True, True, 60, 400_000_000),
            ('fpgaload', True, True, True, 100_000_000),
        ]
        for args in bad_cases:
            with self.subTest(args=args):
                with self.assertRaises(ValueError):
                    build.watchdog_option(*args)


if __name__ == '__main__':
    unittest.main()
