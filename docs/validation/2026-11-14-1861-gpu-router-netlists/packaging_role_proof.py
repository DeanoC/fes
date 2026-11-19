#!/usr/bin/env python3
"""Why every closure digest moved across the base move and no BUILD_ID did.

DeanoC/fes#1951 edited scripts/input_identity.py, which sits in every
registered producer's source closure. So between the two merge bases the whole
closure digest moves for all twenty rows, while the design-role half -- the one
a design-v1 BUILD_ID is taken over -- is byte-identical for all but fes.pcxt-dos
(whose RTL main really did change).

"That file is packaging role, so it cannot move an identity" is a claim about
code, and this program makes the code say it instead of me. It builds a minimal
format-2 record in each tree, then perturbs one entry of its source closure at
a time and reports whether the identity functional_key() derives moves:

  * the packaging-role entry main changed -- must NOT move it;
  * a design-role entry -- must move it, or the first result proves nothing.

The second case is the point. A checker that reported "unchanged" because it
was differencing nothing at all would pass the first case too.

  python3 packaging_role_proof.py --tree <dir> [--tree <dir> ...] [--json out]
"""
from __future__ import annotations

import argparse
import importlib
import json
import os
import subprocess
import sys
from pathlib import Path

MISTEROSS = "sources/misteross"
PACKAGING = "scripts/input_identity.py"
DESIGN = "cores/fes-menu/rtl/fes_menu_top.sv"


def _load(tree: Path):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    root = (tree / MISTEROSS).resolve()
    sys.path.insert(0, str(root))
    os.chdir(root)
    try:
        return importlib.import_module("scripts.input_identity")
    finally:
        sys.path.remove(str(root))


def _fields(closure: dict) -> dict:
    return {"format": 2, "toolchain": "quartus", "repository": "x", "revision": "y",
            "source_path": MISTEROSS, "source_roots": ["cores", "scripts"],
            "source_inputs": dict(closure), "recipe": "r", "recipe_sha256": "0" * 64,
            "dependencies": {}, "tools": {"yosys": "t"},
            "parameters": {"core_id": "probe", "seed": 1}}


def _identity(module, closure: dict) -> str:
    # with_identities() is what a producer calls before encoding its record,
    # and functional_key() is what build_identity() reduces a format-2 record
    # to. Going through both is what makes this the real BUILD_ID path rather
    # than a digest of my own choosing.
    return module.functional_key(module.with_identities(_fields(closure)))


def measure(tree: Path) -> dict:
    module = _load(tree)
    base = {PACKAGING: "a" * 64, DESIGN: "b" * 64, "scripts/bootstrap.sh": "c" * 64}
    roles = {path: module.input_role(path) for path in base}
    identity = _identity(module, base)
    moved = {}
    for path in (PACKAGING, DESIGN):
        perturbed = dict(base, **{path: "f" * 64})
        moved[path] = _identity(module, perturbed) != identity
    return {"revision": subprocess.run(("git", "-C", str(tree), "rev-parse", "HEAD"),
                                       capture_output=True, text=True).stdout.strip(),
            "roles": roles, "identity": identity, "moves_identity": moved}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree", type=Path, action="append", required=True)
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    if arguments.json is not None:
        arguments.json = arguments.json.resolve()   # before any chdir
    result, ok = {}, True
    for tree in arguments.tree:
        row = measure(tree.resolve())
        result[str(tree)] = row
        packaging_ok = row["roles"][PACKAGING] == "packaging" and not row["moves_identity"][PACKAGING]
        design_ok = row["roles"][DESIGN] == "design" and row["moves_identity"][DESIGN]
        ok = ok and packaging_ok and design_ok
        print(f"{row['revision'][:12]}  {tree}")
        print(f"  {PACKAGING}: role={row['roles'][PACKAGING]} "
              f"moves BUILD_ID={row['moves_identity'][PACKAGING]} "
              f"-> {'as expected' if packaging_ok else 'UNEXPECTED'}")
        print(f"  {DESIGN}: role={row['roles'][DESIGN]} "
              f"moves BUILD_ID={row['moves_identity'][DESIGN]} "
              f"-> {'as expected' if design_ok else 'UNEXPECTED'}")
    print()
    print("a packaging-role byte change cannot move a design-v1 BUILD_ID; "
          "a design-role one does" if ok else "REFUSED: the control did not behave")
    if arguments.json:
        arguments.json.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json}")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
