#!/usr/bin/env python3
"""The Verilator lint configuration simulation builds pass after their sources.

sim/verilator-5054.vlt waives, file by file or signal by signal, lint that
Verilator 5.054 raises on code 5.032 accepted. Older releases reject a
configuration naming a rule they do not know, so the file goes only to a
Verilator that knows all of RULES. Run as a script, it prints the arguments
for the Makefile.
"""
from __future__ import annotations

import functools
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / 'sim/verilator-5054.vlt'
RULES = ('PROCASSINIT', 'SIMILARNAME', 'UNOPTPUBLIC', 'ALWNEVER', 'UNDRIVEN', 'MULTIDRIVEN')


@functools.cache
def waivers(verilator: str = 'verilator') -> tuple[str, ...]:
    probe = [verilator, *(f'-Wno-{rule}' for rule in RULES), '--version']
    known = subprocess.run(probe, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    return (str(CONFIG),) if known else ()


if __name__ == '__main__':
    print(' '.join(waivers(*sys.argv[1:2])))
