#!/usr/bin/env python3
"""qsys_equivalence.py REF_PROJECT PROJECT...: each rung's generated project against a reference rung's.

Every project is a rung's generated project as its producer left it, before the shared-reset
derivation (the producer stopped at that gate). Over every .v, .sv, .sdc and .vh outside qdb/,
output_files/ and the PLL's own fes_mailbox/machine_pll/, with the Qsys
`mm_interconnect_1920_<hash>` names that the two projects do not share (the window's machine-clock
interconnect: one per project, checked) replaced by one token in paths and contents, a file is reported
when it differs from the reference's. Expected, and labelled so: the two netlist copies
(pc386_system.v) and the scanout component's two wrappers, whose only difference must be the
BUILD_ID parameter line (checked). Anything else is UNEXPECTED. Exit 1 on any unexpected
difference.
"""
import re
import sys
from pathlib import Path

NAME = re.compile(rb'mm_interconnect_1920_[a-z0-9]{7}')
SKIP = ('qdb/', 'output_files/', 'fes_mailbox/machine_pll/')
NETLISTS = {'fes_mailbox/pc386_system.v',
            'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_scanout/fes_de25_pc386_standard_10/synth/pc386_system.v'}
WRAPPERS = {'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_scanout/synth/peripheral_subsys_fes_scanout.v',
            'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_scanout/peripheral_subsys_fes_scanout_bb.v'}


def hashes(project):
    return {m.group(0) for p in Path(project).rglob('*mm_interconnect_1920_*') for m in [NAME.search(p.name.encode())] if m}


def files(project, own):
    out = {}
    for p in Path(project).rglob('*'):
        rel = p.relative_to(project).as_posix()
        if p.is_file() and p.suffix in ('.v', '.sv', '.sdc', '.vh') and not rel.startswith(SKIP):
            data = p.read_bytes().replace(own, b'IC')
            out[rel.encode().replace(own, b'IC').decode()] = data
    return out


def main(ref, *projects):
    bad = False
    ref_names = hashes(ref)
    for project in projects:
        names = hashes(project)
        a_only, b_only = ref_names - names, names - ref_names
        if len(a_only) != 1 or len(b_only) != 1:
            print(f'{project}: UNEXPECTED interconnect names, {sorted(a_only)} against {sorted(b_only)}')
            bad = True
            continue
        base = files(ref, next(iter(a_only)))
        mine = files(project, next(iter(b_only)))
        notes = []
        for rel in sorted(set(base) | set(mine)):
            a, b = base.get(rel), mine.get(rel)
            if a == b:
                continue
            if rel in NETLISTS and a is not None and b is not None:
                notes.append(f'  expected  netlist  {rel}')
            elif rel in WRAPPERS and a is not None and b is not None and \
                    [x for x in a.splitlines() if b'BUILD_ID' not in x] == [x for x in b.splitlines() if b'BUILD_ID' not in x]:
                notes.append(f'  expected  BUILD_ID only  {rel}')
            else:
                bad = True
                notes.append(f'  UNEXPECTED  {rel}')
        print(f'{project}: {len(mine)} files, {sum("UNEXPECTED" in n for n in notes)} unexpected')
        print('\n'.join(notes))
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
