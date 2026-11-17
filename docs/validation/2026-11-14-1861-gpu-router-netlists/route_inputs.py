#!/usr/bin/env python3
"""Are each artefact's route-deciding inputs byte-identical across two trees?

D12's merge-forward clause and the check_level rule both turn on the same set:
"its toolchain lock, its timing and pin constraints, and its placement recipe".
Comparing build_commands() is not enough to cover that set, because several
producers (spectrum, ramtest, c64, coleco) ignore the route tuple
build_commands returns and call route_after_synth with their own arguments
read from pnr_recipe. So this reads the recipe through the producer's own
pnr_recipe digests instead of restating any of it.

  python3 route_inputs.py --tree-a <dir> --tree-b <dir> [--json out.json]
"""
from __future__ import annotations

import argparse
import hashlib
import importlib
import json
import os
import sys
from pathlib import Path

MISTEROSS = "sources/misteross"

# artefact -> (producer module, core_id or None, attribute names for lock/qsf/sdc)
CASES = (
    ("fes.menu", "build_fes_menu_package", "fes.menu"),
    ("splash", "build_fes_splash", None),
    ("fes.pong", "build_fes_pong", "fes.pong"),
    ("fes.ramtest", "build_fes_ramtest", "fes.ramtest"),
    ("fes.riscv", "build_fes_riscv", "fes.riscv"),
    ("fes.coleco", "build_fes_coleco_socket_v2", "fes.coleco"),
    ("fes.c64", "build_fes_c64_oss", "fes.c64"),
    ("fes.spectrum", "build_fes_spectrum_oss", "fes.spectrum"),
    ("fes.spectrum128", "build_fes_spectrum_oss", "fes.spectrum128"),
    ("fes.spectrum-plus3", "build_fes_spectrum_oss", "fes.spectrum-plus3"),
    ("fes.pentagon", "build_fes_spectrum_oss", "fes.pentagon"),
    ("fes.zx81", "build_fes_zx81_quartus", "fes.zx81"),
    ("fes.zx80", "build_fes_zx81_quartus", "fes.zx80"),
    ("fes.ts1000", "build_fes_zx81_quartus", "fes.ts1000"),
    ("fes.ts1500", "build_fes_zx81_quartus", "fes.ts1500"),
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


def _file(root: Path, relative) -> str | None:
    if relative is None:
        return None
    path = root / str(relative)
    if not path.is_file():
        return f"absent:{relative}"
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _declared(producer) -> tuple[str, ...]:
    """The paths the producer itself says decide its build."""
    for name in ("PINNED_INPUTS", "INPUTS", "SOURCE_ROOTS"):
        value = getattr(producer, name, None)
        if value:
            return tuple(str(item) for item in value)
    if hasattr(producer, "inputs_for"):
        return tuple(str(item) for item in producer.inputs_for())
    return ()


def _pick(producer, *suffixes: str) -> str | None:
    """Find the one declared input with this role, or refuse to guess.

    Reading an attribute by a guessed name is how this program first went
    wrong: six producers keep their lock under a prefixed name
    (MENU_TOOLCHAIN_LOCK, C64_TOOLCHAIN_LOCK, SPECTRUM_TOOLCHAIN_LOCK) or in
    no constant at all, so a `getattr(..., "TOOLCHAIN_LOCK", None) or
    "toolchain.lock"` chain silently fell back to the repository-root lock and
    reported six different cores as sharing one digest. Selecting from the
    producer's own declared input list cannot drift that way, and returning
    None when the match is not unique makes the gap visible as UNANSWERED
    instead of as a passing comparison.
    """
    hits = sorted({path for path in _declared(producer)
                   if any(path.endswith(suffix) for suffix in suffixes)})
    return hits[0] if len(hits) == 1 else None


def measure(tree: Path, module: str, core_id: str | None) -> dict:
    producer = _load(tree, module)
    root = producer.ROOT
    lock = _pick(producer, "toolchain.lock", "quartus.lock")
    qsf = _pick(producer, ".qsf")
    sdc = _pick(producer, ".sdc")
    out = {
        "toolchain_lock_path": lock or "unavailable:not-uniquely-declared",
        "toolchain_lock": _file(root, lock) if lock else "unavailable:not-uniquely-declared",
        "qsf_path": qsf or "unavailable:not-uniquely-declared",
        "qsf": _file(root, qsf) if qsf else "unavailable:not-uniquely-declared",
        "sdc_path": sdc or "unavailable:not-uniquely-declared",
        "sdc": _file(root, sdc) if sdc else "unavailable:not-uniquely-declared",
    }
    if core_id is not None:
        from scripts import pnr_recipe
        for name in ("recipe_digest", "design_recipe_digest"):
            try:
                out[name] = getattr(pnr_recipe, name)(core_id)
            except Exception as exc:
                out[name] = f"unavailable:{type(exc).__name__}"
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree-a", type=Path, required=True)
    parser.add_argument("--tree-b", type=Path, required=True)
    parser.add_argument("--json", type=Path)
    args = parser.parse_args()
    a, b = args.tree_a.resolve(), args.tree_b.resolve()
    result, moved, unanswered = {}, [], []
    print(f'{"artefact":22} {"lock":6} {"qsf":6} {"sdc":6} {"recipe":8} {"design":8}')
    for artefact, module, core_id in CASES:
        try:
            ma, mb = measure(a, module, core_id), measure(b, module, core_id)
        except ModuleNotFoundError as exc:
            # A producer that exists on only one side cannot be compared. Say
            # so rather than skipping it, because a missing row here means the
            # artefact changed producer, which is itself a reason to re-measure.
            print(f"{artefact:22} PRODUCER ABSENT ON ONE SIDE: {exc.name}")
            unanswered.append(f"{artefact}: producer absent on one side")
            result[artefact] = {"verdict": "producer absent on one side"}
            continue
        row, bad = {}, []
        for key in sorted(set(ma) | set(mb)):
            if key.endswith("_path"):
                continue
            va, vb = ma.get(key), mb.get(key)
            if isinstance(va, str) and va.startswith("unavailable:"):
                unanswered.append(f"{artefact}.{key}")
                row[key] = "?"
                continue
            row[key] = "same" if va == vb else "MOVED"
            if va != vb:
                bad.append(key)
        if bad:
            moved.append(f"{artefact}: {', '.join(bad)}")
        result[artefact] = {"tree_a": ma, "tree_b": mb, "verdict": row}
        print(f'{artefact:22} {row.get("toolchain_lock","-"):6} {row.get("qsf","-"):6} '
              f'{row.get("sdc","-"):6} {row.get("recipe_digest","-"):8} '
              f'{row.get("design_recipe_digest","-"):8}')
    print()
    if unanswered:
        print("UNANSWERED: " + ", ".join(unanswered))
    print("every route-deciding input is byte-identical" if not moved
          else "MOVED -- " + "; ".join(moved))
    if args.json:
        args.json.write_text(json.dumps(
            {"tree_a": str(a), "tree_b": str(b), "rows": result,
             "moved": moved, "unanswered": unanswered}, indent=2, sort_keys=True) + "\n")
        print(f"wrote {args.json}")
    return 1 if (moved or unanswered) else 0


if __name__ == "__main__":
    raise SystemExit(main())
