#!/usr/bin/env python3
"""Build the standard DE25 Atari 520ST; retain candidates and never program hardware.

The machine and its gates are in build_machine_package.py. The user's TOS is
not part of the package: it is uploaded at launch.
"""
from __future__ import annotations
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers.build_machine_package import ABI, Machine
from boards.de25nano.producers.build_menu_package import execution_environment, execution_inputs  # noqa: F401

RECIPE = 'boards/de25nano/producers/build_atari_st_package.py'
MACHINE = Machine(
    recipe=RECIPE, directory='atari-st', core_id='fes.atari-st', rom_id='atari-st-firmware',
    name='Atari 520ST', version='1.0.0',
    descriptions={
        '520st': 'Standard DE25 Atari 520ST with uploaded 192 KiB firmware, board SDRAM, '
                 'a writable variable-geometry floppy and a relative mouse'},
    interfaces=tuple((name, 1, 0) for name in (
        'fes.mouse.relative', 'fes.media.atari-st-floppy', 'fes.media.atari-st-floppy-write',
        'fes.media.atari-st-floppy-geometry')))


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
