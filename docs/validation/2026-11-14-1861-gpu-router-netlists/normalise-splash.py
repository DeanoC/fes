#!/usr/bin/env python3
"""Apply D12's src-path exemption to the splash comparison, and nothing else.

Splash is the one affected artifact that cannot share a tool install between
the two sides: it refuses a shared cache root and authenticates a local install
at <tree>/build/toolchain. Yosys records that install's path in the `src`
attribute of cells it reads from its own share/yosys models, so the two
netlists differ in those strings and nowhere else.

D12 exempts exactly this -- "file paths in `src` attributes (the checkout and
the tool install directory: a toolchain at the same commit is the same
toolchain wherever it is installed)". The two installs here are byte-identical
binaries of the same commit, which is checked separately.

This replaces each side's tree prefix with a placeholder and compares. It
applies no other normalisation: any difference that is not a tree path survives
and makes the digests differ.

  python3 normalise-splash.py <main-tree> <branch-tree> <pass-dir>...
"""
import hashlib
import sys

def normalised(path: str, root: str) -> str:
    return open(path).read().replace(root, "<TREE>")

def main(argv) -> int:
    main_tree, branch_tree, *passes = argv[1:]
    failures = 0
    for directory in passes:
        digests = {}
        for side, root in (("main", main_tree), ("branch", branch_tree)):
            text = normalised(f"{directory}/splash-{side}/synth.json", root)
            digests[side] = hashlib.sha256(text.encode()).hexdigest()
        same = digests["main"] == digests["branch"]
        print(f"{directory}: main={digests['main'][:16]} branch={digests['branch'][:16]} "
              f"{'IDENTICAL' if same else 'DIFFERS'}")
        failures += not same
    return 1 if failures else 0

if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
