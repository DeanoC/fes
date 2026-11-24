"""Tests for opt-in DE25-Nano fpgaload DHCP/SNTP build option validation."""
import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SHIPPED_FPGALOAD_PROFILE = ROOT / 'platform/embedded/nano/sd-profile.json'
OPT_INS = [
    (True, '', '', 0),
    (True, 'fes-de25', '', 0),
    (False, '', '10.0.2.2', 0),
    (False, '', '10.0.2.2', 123),
    (True, 'fes-de25', '10.0.2.2', 123),
]


def _load_build():
    path = ROOT / 'platform/embedded/tamago/build.py'
    spec = importlib.util.spec_from_file_location('fes_tamago_build_net_test', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _admitted_network():
    """A network block profile_constants admits beside the shipped RAM and reservations."""
    return {'mac_index': 0, 'phy_address': 4, 'phy_id': 0x001cc916,
            'mac_address': '02:12:34:56:78:90', 'csr_clock_hz': 125000000,
            'rx_buffer_shift': 4, 'address': '192.0.2.2/24', 'gateway': '192.0.2.1',
            'dma_start': 0x9a000000, 'dma_size': 2 << 20, 'dma_identity': True,
            'rgmii_125mhz_ready': True}


class TamagoNetworkBuildOptionTests(unittest.TestCase):
    def test_network_runtime_options_validation(self):
        build = _load_build()
        # Synthetic profiles exercise the validator only; no shipped fpgaload
        # profile enables network (see the dormancy test below).
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

    def test_opt_in_is_dormant_on_the_shipped_fpgaload_profile(self):
        """network/README.md documents the opt-in as dormant; this pins why.

        If any route below starts accepting, a network-enabled fpgaload profile
        has become buildable and the README's dormant section must be revised.
        """
        build = _load_build()
        self.assertEqual(build.APP.parent / 'nano' / 'sd-profile.json', SHIPPED_FPGALOAD_PROFILE)
        shipped = json.loads(SHIPPED_FPGALOAD_PROFILE.read_bytes())

        # Route 1: the shipped profile disables Ethernet, so every opt-in fails.
        self.assertIsNone(build.profile_constants(shipped)[5])
        self.assertEqual(
            build.network_runtime_options('fpgaload', shipped, True, True, False, '', '', 0),
            (False, '', '', 0),
        )
        for opt_in in OPT_INS:
            with self.subTest(route='shipped profile', opt_in=opt_in):
                with self.assertRaisesRegex(ValueError, 'dormant'):
                    build.network_runtime_options('fpgaload', shipped, True, True, *opt_in)

        # Route 2: adding Ethernet to the shipped profile breaks its SD ownership.
        with_network = copy.deepcopy(shipped)
        with_network['network'] = _admitted_network()
        with_network['handoff']['ethernet_platform_ready'] = True
        with self.assertRaisesRegex(ValueError, 'closed-bridge SD ownership'):
            build.profile_constants(with_network)

        # Route 3: a network-only profile passes admission and the option check,
        # but build() accepts fpgaload only with the byte-exact shipped profile.
        network_only = copy.deepcopy(with_network)
        network_only['sd_control'] = None
        network_only['sd_input_clock_hz'] = 0
        self.assertIsNotNone(build.profile_constants(network_only)[5])
        for opt_in in OPT_INS:
            with self.subTest(route='network-only profile', opt_in=opt_in):
                build.network_runtime_options('fpgaload', network_only, True, True, *opt_in)
        with tempfile.TemporaryDirectory() as tmp:
            profile_path = Path(tmp) / 'network-profile.json'
            profile_path.write_text(json.dumps(network_only))
            output = Path(tmp) / 'never-built'
            for opt_in in OPT_INS:
                dhcp, hostname, sntp_server, sntp_port = opt_in
                args = argparse.Namespace(
                    interactive=False, disk=None, qemu=None, display=None,
                    program='fpgaload', profile=profile_path, package_id=None,
                    computer_control=True, computer_reload=True, computer_storage_return=True,
                    selected_programs=True, tenfoot_serial=True,
                    network_dhcp=dhcp, network_dhcp_hostname=hostname,
                    network_sntp_server=sntp_server, network_sntp_port=sntp_port,
                    hps_first_project=Path(tmp), output=output)
                with self.subTest(route='build pin', opt_in=opt_in):
                    with mock.patch.dict(os.environ, {'FES_EXPERIMENTAL_DE25_EMBEDDED': '1'}):
                        with self.assertRaisesRegex(ValueError, 'source-derived serial/SD profile'):
                            build.build(args)
            self.assertFalse(output.exists())


if __name__ == '__main__':
    unittest.main()
