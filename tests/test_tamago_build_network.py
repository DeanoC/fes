"""Tests for opt-in DE25-Nano fpgaload DHCP/SNTP build option validation."""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


def _load_build():
    path = ROOT / 'platform/embedded/tamago/build.py'
    spec = importlib.util.spec_from_file_location('fes_tamago_build_net_test', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class TamagoNetworkBuildOptionTests(unittest.TestCase):
    def test_network_runtime_options_validation(self):
        build = _load_build()
        net_profile = {'network': {'address': '10.0.2.15/24'}}
        no_net_profile = {'network': None}

        # Defaults stay disabled on any program/profile.
        self.assertEqual(
            build.network_runtime_options('probe', no_net_profile, False, False, False, '', '', 0),
            (False, '', '', 0),
        )
        self.assertEqual(
            build.network_runtime_options('fpgaload', no_net_profile, True, True, None, None, None, None),
            (False, '', '', 0),
        )

        # Valid opt-in combinations.
        self.assertEqual(
            build.network_runtime_options('fpgaload', net_profile, True, True, True, 'fes-de25', '10.0.2.2', 0),
            (True, 'fes-de25', '10.0.2.2', 0),
        )
        self.assertEqual(
            build.network_runtime_options('fpgaload', net_profile, True, True, False, '', '192.168.1.10', 1234),
            (False, '', '192.168.1.10', 1234),
        )

        # Invalid gates/arguments fail closed.
        bad_cases = [
            ('probe', net_profile, True, True, True, '', '', 0),
            ('fpgaload', net_profile, False, True, True, '', '', 0),
            ('fpgaload', net_profile, True, False, True, '', '', 0),
            ('fpgaload', no_net_profile, True, True, True, '', '', 0),
            ('fpgaload', no_net_profile, True, True, False, '', '10.0.2.2', 0),
            ('fpgaload', net_profile, True, True, False, 'fes-de25', '', 0),
            ('fpgaload', net_profile, True, True, True, 'bad host', '', 0),
            ('fpgaload', net_profile, True, True, True, 'a' * 256, '', 0),
            ('fpgaload', net_profile, True, True, False, '', '0.0.0.0', 0),
            ('fpgaload', net_profile, True, True, False, '', '224.0.0.1', 0),
            ('fpgaload', net_profile, True, True, False, '', '::1', 0),
            ('fpgaload', net_profile, True, True, False, '', 'not-an-ip', 0),
            ('fpgaload', net_profile, True, True, False, '', '', 123),
            ('fpgaload', net_profile, True, True, False, '', '10.0.2.2', 65536),
            ('fpgaload', net_profile, True, True, False, '', '10.0.2.2', -1),
            ('fpgaload', net_profile, True, True, 'yes', '', '', 0),
            ('fpgaload', net_profile, True, True, False, '', '10.0.2.2', True),
        ]
        for args in bad_cases:
            with self.subTest(args=args):
                with self.assertRaises(ValueError):
                    build.network_runtime_options(*args)


if __name__ == '__main__':
    unittest.main()
