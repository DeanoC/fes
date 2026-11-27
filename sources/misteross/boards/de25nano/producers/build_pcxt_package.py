#!/usr/bin/env python3
"""Build the standard DE25 PC/XT DOS machine; retain candidates and never program hardware.

The machine and its gates are in build_machine_package.py. GLaBIOS and the
CGA/MDA fonts are not part of the package: they are uploaded at launch as one
73,728-byte image: the 64 KiB system ROM window, the 4 KiB CGA font, and
the 4 KiB MDA font.
"""
from __future__ import annotations
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers.build_machine_package import ABI, Machine
from boards.de25nano.producers.build_menu_package import execution_environment, execution_inputs  # noqa: F401

RECIPE = 'boards/de25nano/producers/build_pcxt_package.py'
# The DE10 package's identity and interfaces: the PC/XT floppy and its write
# extension are fes.computer 1.1 interfaces.
MACHINE = Machine(
    recipe=RECIPE, directory='pcxt', core_id='fes.pcxt-dos', rom_id='pcxt-firmware',
    name='FES PC/XT FreeDOS', version='0.1.0',
    descriptions={
        'xt': 'Standard DE25 PC/XT with uploaded GLaBIOS, CGA/MDA fonts, board SDRAM, '
              'a writable 360 KiB floppy, CGA/MDA/Hercules video selection, and the XT keyboard'},
    interfaces=(('fes.media.pcxt-floppy', 1, 0), ('fes.media.pcxt-floppy-write', 1, 0),
                ('fes.persistence.words', 1, 0), ('fes.pcxt.machine', 1, 0)),
    abi_minor=1)


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
