#!/usr/bin/env python3
"""Build the standard DE25 Commodore 64; retain candidates and never program hardware.

The machine and its gates are in build_machine_package.py. The user's BASIC,
KERNAL, character and 1541 ROMs are not part of the package: they are
uploaded at launch as one 36 KiB image.
"""
from __future__ import annotations
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers.build_machine_package import ABI, Machine
from boards.de25nano.producers.build_menu_package import execution_environment, execution_inputs  # noqa: F401

RECIPE = 'boards/de25nano/producers/build_c64_package.py'
MACHINE = Machine(
    recipe=RECIPE, directory='c64', core_id='fes.c64', rom_id='c64-firmware',
    name='Commodore 64', version='1.0.0',
    descriptions={
        'pal': 'Standard DE25 PAL Commodore 64 with uploaded 36 KiB firmware, '
               'a native 1541 and a D64 in block RAM'},
    interfaces=(('fes.media.c64-disk', 1, 0),),
    guest_memory='fabric-bram')


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
