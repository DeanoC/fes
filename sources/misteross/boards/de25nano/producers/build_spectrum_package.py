#!/usr/bin/env python3
"""Build the standard DE25 ZX Spectrum; retain candidates and never program hardware.

The machine and its gates are in build_machine_package.py. The user's ROM is
not part of the package: it is uploaded at launch.
"""
from __future__ import annotations
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers.build_machine_package import ABI, Machine
from boards.de25nano.producers.build_menu_package import execution_environment, execution_inputs  # noqa: F401

RECIPE = 'boards/de25nano/producers/build_spectrum_package.py'
MACHINE = Machine(
    recipe=RECIPE, directory='spectrum', core_id='fes.spectrum', rom_id='spectrum-firmware',
    name='ZX Spectrum', version='1.0.0',
    descriptions={
        '48': 'Standard DE25 ZX Spectrum 48K with uploaded firmware, board SDRAM and a PZX cassette',
        '128': 'Standard DE25 ZX Spectrum 128K with uploaded firmware, board SDRAM and a PZX cassette',
        'plus3': 'Standard DE25 ZX Spectrum +2A/+3 with uploaded firmware, board SDRAM, a PZX cassette '
                 'and a uPD765A +3 disk drive',
        'pentagon': 'Standard DE25 Pentagon 128 with uploaded firmware, board SDRAM, a PZX cassette '
                    'and a Beta 128 TR-DOS drive'},
    interfaces=(('fes.media.spectrum-tape', 2, 0),),
    model_core_ids={'128': 'fes.spectrum128', 'plus3': 'fes.spectrum-plus3', 'pentagon': 'fes.pentagon'},
    model_rom_ids={'128': 'spectrum128-firmware', 'plus3': 'spectrum-plus3-firmware',
                   'pentagon': 'pentagon-firmware'},
    # The disk unit, and the controller that names its image format, as on DE10.
    model_interfaces={'plus3': (('fes.media.spectrum-disk', 1, 0),
                                ('fes.media.spectrum-disk-plus3', 1, 0, False)),
                      'pentagon': (('fes.media.spectrum-disk', 1, 0),
                                   ('fes.media.spectrum-disk-trdos', 1, 0, False))})


def _require_clean_source(root, *, identity_version=2, pinned_inputs=(RECIPE, ABI)):
    return MACHINE.require_clean_source(root)


def _authenticate_tools(root, *, cache_root=None):
    return MACHINE.authenticate(root)


create_build_record = MACHINE.create_build_record
manifest = MACHINE.manifest
build = MACHINE.build


def main(argv=None):
    MACHINE.main(__doc__, argv)


if __name__ == '__main__':
    main()
