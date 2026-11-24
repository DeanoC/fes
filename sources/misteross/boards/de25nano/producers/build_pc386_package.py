#!/usr/bin/env python3
"""Build the standard DE25 pc386; retain candidates and never program hardware.

The machine and its gates are in build_machine_package.py. Its memory is a
window in HPS DDR (fes.fabric.de25-hps-window), and its BIOS and VGA BIOS are
linked in the netlist, as firmware.lock pins them: the package is format 2,
with no ROM map. The pc386 lock's Yosys elaborates the machine; --cache-root
names its toolchain cache and --gpu-router the lock slot that authenticates
it. Nothing routes on a GPU.
"""
from __future__ import annotations
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers.build_machine_package import ABI, Machine
from boards.de25nano.producers.build_menu_package import execution_environment, execution_inputs  # noqa: F401
from scripts.pc386_quartus import INTERFACES

RECIPE = 'boards/de25nano/producers/build_pc386_package.py'
# The DE10 package's ABI (fes.computer 1.1) and interfaces, all 1.0; the board
# adds the window and the scanout fabric.
MACHINE = Machine(
    recipe=RECIPE, directory='pc386', core_id='fes.pc386', rom_id=None,
    name='FES pc386', version='0.1.0',
    descriptions={'386': 'Standard DE25 pc386: fes_i386_cpu with no 387, the Bochs BIOS and LGPL VGABios, '
                         'all memory in a window in HPS DDR. Development only.'},
    interfaces=tuple((name, 1, 0) for name in INTERFACES), shell=(), abi_minor=1)


def _require_clean_source(root, *, identity_version=2, pinned_inputs=(RECIPE, ABI)):
    return MACHINE.require_clean_source(root)


def _authenticate_tools(root, *, cache_root=None):
    return MACHINE.authenticate(root, cache_root=cache_root)


create_build_record = MACHINE.create_build_record
manifest = MACHINE.manifest
build = MACHINE.build


def main(argv=None):
    MACHINE.main(__doc__, argv)


if __name__ == '__main__':
    main()
