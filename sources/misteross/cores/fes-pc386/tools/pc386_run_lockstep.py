#!/usr/bin/env python3
"""Run the unchanged stock full suite with one private output directory."""
import argparse
import os
import stat
from pathlib import Path
import sys

ROOT = Path(__file__).absolute().parents[3]
if ROOT.resolve() != ROOT:
    raise SystemExit('pc386 source root must not traverse a symlink')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args, suite_args = parser.parse_known_args()
    output = args.output.absolute()
    parent = ROOT / 'build/sim/fes-i386-lockstep-runs'
    if (output.parent != parent or not output.name.startswith('run-') or output.is_symlink()
            or not output.is_dir() or output.resolve() != output or any(output.iterdir())):
        raise ValueError('lockstep output must be a fresh private run directory')
    expected = ROOT / 'scripts/test_fes_i386_lockstep.py'
    current = ROOT
    parts = expected.relative_to(ROOT).parts
    for index, part in enumerate(parts):
        current = current / part
        if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
            raise ValueError('lockstep runner must be regular and non-symlinked')
    metadata = output.lstat()
    if metadata.st_uid != os.getuid() or stat.S_IMODE(metadata.st_mode) & 0o077:
        raise ValueError('lockstep run directory must be private and owned by this user')
    sys.path.insert(0, str(ROOT))
    from scripts import test_fes_i386_lockstep as suite
    if Path(suite.__file__).resolve() != expected:
        raise ValueError('lockstep runner differs from the selected stock source')
    # The stock helpers otherwise share mutable build trees across runs and
    # delete stale oracle inputs. Keep both oracle and corpus private too.
    suite.OUTPUT = output
    suite.oracle.BUILD = output / 'oracle'
    suite.vectors.OUTPUT = output / 'vectors'
    sys.argv = [str(expected), *suite_args]
    return suite.main()


if __name__ == '__main__':
    raise SystemExit(main())
