#!/usr/bin/env python3
"""Build the DE25 managed board-SDRAM tester; never program hardware."""
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers.build_machine_package import ABI, Machine
from boards.de25nano.producers.build_menu_package import execution_environment, execution_inputs  # noqa: F401

RECIPE = 'boards/de25nano/producers/build_ramtest_package.py'
MACHINE = Machine(
    recipe=RECIPE, directory='ramtest_managed', core_id='fes.ramtest', rom_id=None,
    name='RAM Tester', version='1.0.0', interfaces=(),
    descriptions={'sdram': 'DE25 128 MiB board SDRAM test; both LPDDR banks are not tested'})


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
