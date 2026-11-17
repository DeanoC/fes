#!/usr/bin/env python3
"""Is each affected artefact's front-end program byte-identical across two trees?

AGENTS.md's check_level rule (#1863) drops the RBF, timing/seal and HIL
conditions for a core recorded `reduced`, but puts that core back under the
full rule for "any change that touches the core's own RTL, programs,
toolchain lock or placement recipe". A producer emits its core's Yosys
program, and this PR edits producers, so the programs have to be shown
unmoved rather than assumed inert.

Each case calls the producer's own build_commands with BUILD_ID pinned to 32
zeros and one identical set of tool paths on both sides, so the only thing
that can differ is the producer's own text. The nextpnr command is compared
too: it carries the placement recipe (seeds, weight, critexp, constraints),
which the same rule names. Nothing is run and nothing is synthesised.

The ZX81-family rows are absent on purpose. #1891 moved them to
build_fes_zx81_quartus, a native Quartus front end with no Yosys program
(its own comment says so), so D12's parenthetical makes the QSF their
condition-1 object; qsf_identity.py answers those.

  python3 front_end_program.py --tree-a <dir> --tree-b <dir> [--dump <dir>]
"""
from __future__ import annotations

import argparse
import hashlib
import importlib
import json
import os
import sys
from pathlib import Path

BUILD_ID = "0" * 32
MISTEROSS = "sources/misteross"
# Identical on both sides. A tool path is a toolchain input, not a program,
# and no tool is run, so these need not exist.
OSS = {"yosys": Path("/nonexistent/tools/yosys"),
       "nextpnr-mistral": Path("/nonexistent/tools/nextpnr-mistral")}


def pong(p):
    return p.build_commands(p.ROOT, p.ROOT / p.OUTPUT_RELATIVE, BUILD_ID, OSS)


def ramtest(p):
    return p.build_commands(p.ROOT, BUILD_ID, OSS)


def riscv(p):
    return p.build_commands(BUILD_ID, OSS)


def menu_package(p):
    return p.build_commands(BUILD_ID, OSS)


def splash(p):
    return p.build_commands(p.ROOT, OSS)


def coleco(p):
    return p.build_commands(p.ROOT, p.ROOT / p.OUTPUT_RELATIVE, BUILD_ID, OSS)


def c64(p):
    return p.build_commands(p.ROOT, p.ROOT / p.OUTPUT_RELATIVE, BUILD_ID, OSS)


def spectrum(model):
    def call(p):
        machine = p.model_for(model)
        return p.build_commands(p.ROOT, p.ROOT / machine.output, BUILD_ID, OSS, model=model)
    return call


def menu(mode):
    def call(p):
        return p.build_commands(OSS, mode=mode)
    return call


# artefact -> (producer module, adapter)
CASES = (
    ("fes.menu", "build_fes_menu_package", menu_package),
    ("fes.menu/diagnostic-ddr", "build_fes_menu", menu("ddr")),
    ("fes.menu/diagnostic-pattern", "build_fes_menu", menu("test-pattern")),
    ("splash", "build_fes_splash", splash),
    ("fes.pong", "build_fes_pong", pong),
    ("fes.ramtest", "build_fes_ramtest", ramtest),
    ("fes.riscv", "build_fes_riscv", riscv),
    ("fes.coleco", "build_fes_coleco_socket_v2", coleco),
    ("fes.c64", "build_fes_c64_oss", c64),
    ("fes.spectrum", "build_fes_spectrum_oss", spectrum("48")),
    ("fes.spectrum128", "build_fes_spectrum_oss", spectrum("128")),
    ("fes.spectrum-plus3", "build_fes_spectrum_oss", spectrum("plus3")),
    ("fes.pentagon", "build_fes_spectrum_oss", spectrum("pentagon")),
)


def _load(tree: Path, module: str):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    root = tree / MISTEROSS
    sys.path.insert(0, str(root))
    os.chdir(root)
    try:
        return importlib.import_module(f"scripts.{module}")
    finally:
        sys.path.remove(str(root))


def render(tree: Path, module: str, adapter):
    commands = adapter(_load(tree, module))
    text = json.dumps([[str(part) for part in command] for command in commands], indent=1)
    return hashlib.sha256(text.encode()).hexdigest(), text


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree-a", type=Path, required=True)
    parser.add_argument("--tree-b", type=Path, required=True)
    parser.add_argument("--dump", type=Path)
    args = parser.parse_args()
    a, b = args.tree_a.resolve(), args.tree_b.resolve()
    failures = 0
    print(f'{"artefact":30} {"producer":28} verdict')
    for artefact, module, adapter in CASES:
        da, ra = render(a, module, adapter)
        db, rb = render(b, module, adapter)
        same = da == db
        failures += 0 if same else 1
        print(f"{artefact:30} {module:28} "
              f"{'identical  ' + da[:16] if same else 'MOVED  ' + da[:16] + ' -> ' + db[:16]}")
        if args.dump:
            args.dump.mkdir(parents=True, exist_ok=True)
            (args.dump / f"{artefact.replace('/', '-')}-a.json").write_text(ra)
            (args.dump / f"{artefact.replace('/', '-')}-b.json").write_text(rb)
    print()
    print("every front-end program and nextpnr command is byte-identical" if not failures
          else f"{failures} case(s) moved")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
