"""Off-board admission for the paired PIO MAC client and its staged arena."""
import argparse
import copy
from pathlib import Path
import sys
import unittest

NANO = Path(__file__).resolve().parents[1] / 'platform/embedded/nano'
sys.path.insert(0, str(NANO))
import launcher_ethernet as ethernet


class PairedEthernetTests(unittest.TestCase):
    def test_pio_selection_and_unchanged_default(self):
        profile = (NANO/'sd-profile.json').read_bytes()
        args = argparse.Namespace(program='fpgaload', paired_ethernet=True, network_dhcp=True,
            c64_disk_swap=True, shared_core_shell=True, tenfoot_serial=True, selected_programs=True)
        self.assertEqual(ethernet.selection(args, profile, profile), ethernet.contract())
        for name in ('network_dhcp', 'shared_core_shell', 'tenfoot_serial', 'selected_programs', 'c64_disk_swap'):
            with self.subTest(name=name), self.assertRaises(ValueError):
                ethernet.selection(argparse.Namespace(**(vars(args) | {name: False})), profile, profile)
        with self.assertRaises(ValueError):
            ethernet.selection(args, (NANO/'sd-dma-profile.json').read_bytes(), profile)
        args.paired_ethernet = False
        self.assertIsNone(ethernet.selection(args, profile, profile))

    def test_compiled_client_and_independent_entry_recheck(self):
        elf = ethernet.ADAPTER.encode()
        base = dict(shared_core_shell=True, selected_programs=True, tenfoot_serial=True, launcher_ethernet=ethernet.contract())
        sdk = dict(base, program='fpgaload', network_dhcp=True, c64_disk_swap=True,
                   command=['go', 'build', '-ldflags', '-X main.expectedNetworkDHCP=1'])
        entry = dict(base, native_program='fpgaload', staging_exclusions=[[0x95c00000,0x95e00000]])
        self.assertEqual(ethernet.checked(sdk, elf), ethernet.checked(entry, elf, entry=True))
        for receipt, as_entry, payload in [(sdk,False,b''), (dict(sdk, network_dhcp=False),False,elf),
                (dict(sdk, sd_adma2=True),False,elf), (dict(sdk,launcher_ethernet=None),False,elf),
                (dict(entry,staging_exclusions=[]),True,elf)]:
            with self.subTest(receipt=receipt), self.assertRaises(ValueError):
                ethernet.checked(receipt, payload, entry=as_entry)
        for field, value in [('dma_bytes', 2<<20),('dma_pages',['RW']*5),('initial_state','ready'),('stream_id',True)]:
            tampered=copy.deepcopy(entry)
            tampered['launcher_ethernet'][field]=value
            with self.subTest(field=field), self.assertRaises(ValueError):
                ethernet.checked(tampered, elf, entry=True)
